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

	// ChunksByIDs 供向量通道按 chunk ID 回填正文与 provenance。
	ChunksByIDs(ctx context.Context, ids []int64) ([]*ent.DocumentChunk, error)
	// SearchChunks 是词法通道：按词元子串匹配已索引的分块。
	SearchChunks(ctx context.Context, terms []string, limit int) ([]*ent.DocumentChunk, error)
}

type Postgres struct {
	client *ent.Client
}

func NewPostgres(client *ent.Client) *Postgres {
	return &Postgres{client: client}
}

var _ Store = (*Postgres)(nil)
