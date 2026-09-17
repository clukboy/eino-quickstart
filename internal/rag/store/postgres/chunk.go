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
// 只取 vector_status=indexed：没进向量库的分块不该被召回，否则检索结果会与
// 向量库内容不一致（chunk 在 PG 里是可见的，但它的向量并不存在）。
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
		Where(
			documentchunk.VectorStatusEQ(documentchunk.VectorStatusIndexed),
			documentchunk.Or(predicates...),
		).
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
