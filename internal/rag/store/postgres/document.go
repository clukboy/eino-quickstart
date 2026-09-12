package postgres

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/ent/document"
	"fmt"
	"strconv"

	"github.com/cloudwego/eino-ext/components/document/loader/file"
	"github.com/cloudwego/eino/schema"
)

func (l *Postgres) CreateDocument(ctx context.Context, docs []*schema.Document) ([]*schema.Document, error) {
	for _, doc := range docs {
		title := fmt.Sprintf("%s%s", doc.MetaData["product_id"], doc.MetaData["product_name"])
		if title == "" {
			return nil, fmt.Errorf("rag create document: title is empty")
		}
		result, err := l.client.Document.Query().Where(document.TitleEQ(title)).Only(ctx)
		if err != nil && !ent.IsNotFound(err) {
			return nil, fmt.Errorf("rag create document: %w", err)
		}
		if ent.IsNotFound(err) {
			result, err = l.client.Document.Create().
				SetTitle(title).
				SetSource(doc.MetaData[file.MetaKeySource].(string)).
				SetMetadata(doc.MetaData).
				Save(ctx)
			if err != nil {
				return nil, fmt.Errorf("rag create document: %w", err)
			}

		} else {
			err = l.client.Document.UpdateOneID(result.ID).
				SetTitle(title).
				SetSource(doc.MetaData[file.MetaKeySource].(string)).
				SetMetadata(doc.MetaData).
				Exec(ctx)
			if err != nil {
				return nil, fmt.Errorf("rag update document: %w", err)
			}
		}

		doc.ID = strconv.FormatUint(result.ID, 10)
	}
	return docs, nil
}

func (l *Postgres) UpdateDocumentStatus(ctx context.Context, docs []*schema.Document) ([]*schema.Document, error) {
	ids := make([]uint64, len(docs))
	for i, doc := range docs {
		id, _ := strconv.ParseUint(doc.ID, 10, 64)
		ids[i] = id
	}
	err := l.client.Document.Update().SetStatus(document.StatusFailed).Where(document.IDIn(ids...)).Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("rag update document status: %w", err)
	}
	return docs, nil
}
