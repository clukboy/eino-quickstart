package postgres

import (
	"context"
	"eino-quickstart/ent"

	"github.com/cloudwego/eino/schema"
)

type Store interface {
	CreateDocument(ctx context.Context, docs []*schema.Document) ([]*schema.Document, error)
	UpdateDocumentStatus(ctx context.Context, docs []*schema.Document) ([]*schema.Document, error)
	CreateChunk(ctx context.Context, docs []*schema.Document) ([]*schema.Document, error)
}

type Postgres struct {
	client *ent.Client
}

func NewPostgres(client *ent.Client) *Postgres {
	return &Postgres{client: client}
}

var _ Store = (*Postgres)(nil)
