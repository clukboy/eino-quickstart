package es

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/elastic/go-elasticsearch/v9/typedapi/core/deletebyquery"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types/enums/conflicts"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types/enums/operationtype"
)

// IndexChunks 把分块写成检索文档，按 _id = chunk_id 覆盖式写入。
//
// 幂等是这里最重要的性质：worker 的重试是「重跑整批」，只要 _id 稳定，
// 半途失败的批次重来一遍就不会留下重复文档。所以 _id 必须显式给，
// 不能交给 ES 自动生成。
//
// 分批提交而不是一次性塞进去：一篇长文档按 batchSize=32 能切出几百个分块，
// 每块正文上千字，一个请求就是几 MB，会顶到集群的 http.max_content_length，
// 而那个失败信息（413）和「文档太长」看起来毫无关系。
func (c *Client) IndexChunks(ctx context.Context, docs []ChunkDoc) error {
	if len(docs) == 0 {
		return nil
	}
	for start := 0; start < len(docs); start += c.bulkSize {
		end := min(start+c.bulkSize, len(docs))
		if err := c.indexBatch(ctx, docs[start:end]); err != nil {
			return fmt.Errorf("es: 写入分块 [%d:%d): %w", start, end, err)
		}
	}
	return nil
}

func (c *Client) indexBatch(ctx context.Context, docs []ChunkDoc) error {
	bulk := c.Bulk().Index(c.index)
	for _, doc := range docs {
		id := doc.ChunkID
		if id == "" {
			// 没有 ID 就没有幂等键，写进去只会变成一条永远清理不掉的孤儿。
			return fmt.Errorf("es: 分块缺少 chunk_id，无法写入索引")
		}
		// 按映射把元数据落进文档、并盖上形态指纹（见 ChunkDoc.enrich）。
		// 放在这里而不是让调用方做：取值规则属于映射，而映射只在这里持有。
		doc.enrich(c.mapping, c.rev)
		if err := bulk.IndexOp(types.IndexOperation{Id_: &id}, doc); err != nil {
			return fmt.Errorf("es: 组装 bulk 请求: %w", err)
		}
	}

	resp, err := bulk.Do(ctx)
	if err != nil {
		return fmt.Errorf("es: 提交 bulk 请求: %w", err)
	}
	if !resp.Errors {
		return nil
	}
	// bulk 的失败是「部分成功」：HTTP 200 但 items 里带 error。不逐条检查的话，
	// 写入会以「成功」返回，然后表现为「某些分块就是搜不到」。
	return fmt.Errorf("es: bulk 部分文档写入失败: %s", bulkFailures(resp.Items))
}

// bulkFailures 摘出前几条失败原因。
//
// 只报前几条：一篇文档的失败原因通常是同一个（mapping 冲突、字段类型不对），
// 几百条重复的错误信息只会把真正有用的那一条淹掉。
func bulkFailures(items []map[operationtype.OperationType]types.ResponseItem) string {
	seen := make(map[string]struct{}, 4)
	reasons := make([]string, 0, 4)
	for _, item := range items {
		for _, result := range item {
			if result.Error == nil {
				continue
			}
			reason := ""
			if result.Error.Reason != nil {
				reason = *result.Error.Reason
			}
			if result.Id_ != nil {
				reason = "chunk " + *result.Id_ + ": " + reason
			}
			if _, exists := seen[reason]; exists {
				continue
			}
			seen[reason] = struct{}{}
			reasons = append(reasons, reason)
		}
		if len(reasons) >= maxReportedBulkErrors {
			break
		}
	}
	if len(reasons) == 0 {
		return "（集群报告 errors=true，但未给出具体条目）"
	}
	return strings.Join(reasons, "; ")
}

// maxReportedBulkErrors 是单次报错里最多列出的条目数。
const maxReportedBulkErrors = 3

// DeleteByDocument 删除一篇文档在索引里的全部分块，返回删除条数。
//
// 它服务于「重新切块」：分块行在 PostgreSQL 里是删掉重建的，新分块会拿到
// 新的自增 ID，老文档的 _id 就再也不会被覆盖 —— 不显式清理就会留下永久
// 残留（检索侧虽然在回查 PostgreSQL 时会跳过它们，但索引会一直涨）。
//
// 删除是尽力而为的，调用方只记警告：即使残留，正确性也不受影响（命中的
// 分块在 PostgreSQL 里查不到就被跳过），所以没必要为此让一次重建整批失败。
func (c *Client) DeleteByDocument(ctx context.Context, documentID uint64) (int64, error) {
	resp, err := c.DeleteByQuery(c.index).
		Request(&deletebyquery.Request{Query: &types.Query{
			Term: map[string]types.TermQuery{
				fieldDocumentID: {Value: strconv.FormatUint(documentID, 10)},
			},
		}}).
		// conflicts=proceed：并发的写入/更新不该让清理失败，剩下的残留
		// 由下一次重建或全量重建收拾。
		Conflicts(conflicts.Proceed).
		// refresh=true：重建之后紧接着可能就有查询（比如评测），不刷新的话
		// 刚删的文档在短时间内仍然可见。
		Refresh(true).
		Do(ctx)
	if err != nil {
		return 0, fmt.Errorf("es: 按文档 %d 清理索引: %w", documentID, err)
	}
	deleted := int64(0)
	if resp.Deleted != nil {
		deleted = *resp.Deleted
	}
	return deleted, nil
}
