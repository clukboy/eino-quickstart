package postgres

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/ent/document"
	"eino-quickstart/ent/documentchunk"
	"eino-quickstart/ent/predicate"
	"eino-quickstart/internal/platform/storage/entx"
	"eino-quickstart/internal/rag/constant"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/schema"
)

// ChunksByIDs 按 chunk ID 批量取回分块，并带出所属文档与数据集。
//
// 这是向量通道回填元数据的唯一入口：Milvus 里只存了 chunk ID 和向量，正文、
// 标题路径、source / visibility / owner 这些引用与授权要用的字段全在 PG 上，
// 不一起取回来就既给不出引用、也做不了 ACL 过滤。
//
// 顺序不保证跟传入的 ids 一致，调用方要按自己的排名重建顺序。
func (l *Postgres) ChunksByIDs(ctx context.Context, ids []int64) ([]*ent.DocumentChunk, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	uids := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			uids = append(uids, uint64(id))
		}
	}
	if len(uids) == 0 {
		return nil, nil
	}
	return l.client.DocumentChunk.Query().
		Where(documentchunk.IDIn(uids...)).
		WithDocument(func(q *ent.DocumentQuery) {
			q.WithDataset()
		}).
		All(ctx)
}

// SearchChunks 是词法通道：按词元做子串匹配，返回候选分块。
//
// 匹配面同时覆盖分块正文、标题路径与文档 source —— 型号这类查询词往往只出现
// 在文件名或标题里，只搜正文会漏。命中数在调用方按词元去重后重新打分排序，
// 所以这里的 limit 是候选上限而非最终条数。
//
// 这里**不按 vector_status 过滤**。词法召回回答的是「哪段文本匹配这个查询」，
// 与这段文本进没进向量库无关。早先这里要求 indexed，等于把关键词召回绑在向量
// 链路的进度上：向量批次失败的文档会连关键词都搜不到，而症状与「召回质量差」
// 一模一样，只看报告看不出来。可见性、属主与启用状态由调用方按 document 的
// 现值判定（见 rag.Store 的 Filter.allows）。
func (l *Postgres) SearchChunks(
	ctx context.Context,
	terms []string,
	limit int,
) ([]*ent.DocumentChunk, error) {
	if len(terms) == 0 || limit <= 0 {
		return nil, nil
	}
	predicates := make([]predicate.DocumentChunk, 0, len(terms)*3)
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		predicates = append(predicates,
			documentchunk.ContentContainsFold(term),
			documentchunk.HeadingPathContainsFold(term),
			documentchunk.HasDocumentWith(document.SourceContainsFold(term)),
		)
	}
	if len(predicates) == 0 {
		return nil, nil
	}
	return l.client.DocumentChunk.Query().
		Where(documentchunk.Or(predicates...)).
		WithDocument(func(q *ent.DocumentQuery) {
			q.WithDataset()
		}).
		Limit(limit).
		All(ctx)
}

func (l *Postgres) CreateChunk(ctx context.Context, docs []*schema.Document) ([]*schema.Document, error) {

	ids := make([]uint64, len(docs))
	for i, doc := range docs {
		id, _ := strconv.ParseUint(doc.ID, 10, 64)
		ids[i] = id
	}
	err := entx.WithTx(ctx, l.client, func(tx *ent.Tx) error {
		_, err := tx.DocumentChunk.Delete().Where(documentchunk.HasDocumentWith(document.IDIn(ids...))).Exec(ctx)
		if err != nil {
			return err
		}
		for _, doc := range docs {
			id, _ := strconv.ParseUint(doc.ID, 10, 64)
			chunkData, err := tx.DocumentChunk.Create().
				SetChunkIndex(doc.MetaData["chunk_index"].(int)).
				SetContent(doc.Content).
				SetDocumentID(id).
				SetHeadingPath(doc.MetaData[constant.MetaHeadingPath].(string)).
				SetMetadata(doc.MetaData).
				Save(ctx)
			if err != nil {
				return err
			}
			doc.MetaData["doc_id"] = id
			doc.ID = strconv.FormatUint(chunkData.ID, 10) // 保持 chunk 的 ID 与 document 一致
		}
		return nil
	})
	return docs, err
}
