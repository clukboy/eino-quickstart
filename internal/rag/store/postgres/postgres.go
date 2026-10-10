package postgres

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/internal/platform/storage/es"

	"github.com/cloudwego/eino/schema"
)

type Store interface {
	CreateDocument(ctx context.Context, docs []*schema.Document) ([]*schema.Document, error)
	UpdateDocumentStatus(ctx context.Context, docs []*schema.Document) ([]*schema.Document, error)
	CreateChunk(ctx context.Context, docs []*schema.Document) ([]*schema.Document, error)

	// ChunksByIDs 供向量通道按 chunk ID 回填正文与 provenance。
	ChunksByIDs(ctx context.Context, ids []int64) ([]*ent.DocumentChunk, error)
	// SearchChunks 是词法通道：按词元子串匹配已索引的分块正文、标题、source
	// 与产品元数据。
	SearchChunks(ctx context.Context, terms []string, limit int) ([]*ent.DocumentChunk, error)
	// SearchExact 在 PostgreSQL 元数据上执行结构化逐字相等匹配，作为无 ES 时的
	// 精确通道回落。字段声明复用 ES mapping，避免两套业务字段清单漂移。
	SearchExact(ctx context.Context, query string, fields []es.ExactField, filter ExactSearchFilter, limit int) ([]es.ChunkHit, error)
}

// ExactSearchFilter 是 PostgreSQL 精确回落需要下推的现值过滤条件。
// 最终授权仍由 rag.Store.reload 再判定一次，避免查询期间状态变化造成放行。
type ExactSearchFilter struct {
	DatasetID  uint64
	Visibility string
	Owner      string
}

type Postgres struct {
	client *ent.Client
}

func NewPostgres(client *ent.Client) *Postgres {
	return &Postgres{client: client}
}

var _ Store = (*Postgres)(nil)
