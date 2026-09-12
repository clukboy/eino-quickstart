package postgres

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/ent/document"
	"eino-quickstart/ent/documentchunk"
	"eino-quickstart/internal/platform/storage/entx"
	"eino-quickstart/internal/rag/constant"
	"strconv"

	"github.com/cloudwego/eino/schema"
)

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
