package knowledge

import (
	"context"
	"fmt"

	"eino-quickstart/ent"
	"eino-quickstart/ent/document"
)

// 这个文件是「正文存在哪」这件事的唯一落点。
//
// 正文只有一处真相：documents.content。它不再有一份对应的磁盘文件，也不再需要
// 调用方自己保管 —— 写进来的是它，召回时读出去的也是它。所以读正文这条路径
// 短得像一句 SQL，而不是「按 source 拼一个路径出来读文件」。
//
// 为什么不像过去那样放一个 ContentStore 在 Service 上：那会把「正文在哪」变成
// 一个可以替换的**部署决定**（文件系统 / 对象存储 / 数据库），而实际上业务对
// 它的要求只有一条 —— 和那篇文档一起原子地存下来。做成可替换的抽象，换来的是
// 「索引读到的正文」与「召回读到的正文」可能来自两个地方，且两边不一致时没有
// 任何东西会报错。

// entDocumentContent 是 DocumentContent 的默认实现，直接读 documents.content。
type entDocumentContent struct {
	client *ent.Client
}

// Contents 一次取回一批文档的正文。
//
// 一次查询取全批：召回补正文是每条命中都要做的一步，逐条查会让往返次数跟着
// top_k 涨（document 粒度下就是 20 次）。用 IDIn 收敛成一次，代价是返回了一张
// 可能只有少数行被用到的 map —— 比多打 19 次数据库便宜。
func (c *entDocumentContent) Contents(ctx context.Context, documentIDs []uint64) (map[uint64]string, error) {
	if c == nil || c.client == nil || len(documentIDs) == 0 {
		return nil, nil
	}
	docs, err := c.client.Document.Query().
		Where(document.IDIn(documentIDs...)).
		Select(document.FieldID, document.FieldContent).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("knowledge: load document contents: %w", err)
	}
	contents := make(map[uint64]string, len(docs))
	for _, doc := range docs {
		contents[doc.ID] = doc.Content
	}
	return contents, nil
}

// documentContents 取这批命中对应的正文，按命中顺序去重后一次查库。
//
// 没有 document_id 的命中（检索侧元数据缺这个键）不参与查询：凭一个 0 去查
// 只会白跑一趟，而结果里那个键永远不存在。
func documentContents(ctx context.Context, reader DocumentContent, hits []SearchHit) (map[uint64]string, error) {
	if reader == nil {
		return nil, nil
	}
	ids := make([]uint64, 0, len(hits))
	seen := make(map[uint64]struct{}, len(hits))
	for _, hit := range hits {
		if hit.DocumentID == 0 {
			continue
		}
		if _, duplicate := seen[hit.DocumentID]; duplicate {
			continue
		}
		seen[hit.DocumentID] = struct{}{}
		ids = append(ids, hit.DocumentID)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return reader.Contents(ctx, ids)
}
