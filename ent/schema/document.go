package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Document holds the schema definition for the Document entity.
type Document struct {
	ent.Schema
}

// Fields of the Document.
func (Document) Fields() []ent.Field {
	return []ent.Field{
		field.String("source"),
		field.String("title"),
		// external_key 是**数据集内**用于查重的稳定业务标识 —— 产品型录填产品
		// 型号（product_id，缺失时退到 model），其他类型留空。
		//
		// 落成独立列而不是从 metadata 的 JSON 里查：JSON 路径既走不了索引，
		// 也加不了唯一约束，而「先查后写」在并发上传下一定会漏。可空是有意的
		// —— Postgres 的唯一索引把 NULL 视为互不相同，所以不参与查重的文档
		// 可以有任意多条，不会互相挡住。
		field.String("external_key").Optional().Nillable(),
		field.JSON("metadata", map[string]any{}).Optional(),
		//field.String("checksum"),
		field.String("owner_subject").Default("system"),
		field.Enum("visibility").
			Values("system", "private").
			Default("system"),
		field.Enum("status").Values("ready", "indexing", "failed", "deleted").Default("indexing"),
		field.Bool("enabled").Default(true),
		field.Uint64("dataset_id").Default(0),
		field.Uint64("folder_id").Optional().Nillable(),

		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Edges of the Document.
func (Document) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("dataset", Dataset.Type).
			Ref("documents").
			Field("dataset_id").
			Unique().
			Required(),

		edge.To("chunks", DocumentChunk.Type),
	}
}

func (Document) Indexes() []ent.Index {
	return []ent.Index{
		//index.Fields("dataset_id", "source").Unique(),
		// 「同一数据集内同一个型号只有一条文档」的兜底：应用层上传时先查后写
		// 能给出 created / updated 的语义，但两个并发请求会同时查不到、同时
		// 插入；唯一索引让其中一个拿到约束冲突，再退化成更新。
		index.Fields("dataset_id", "external_key").Unique(),
		index.Fields("owner_subject", "visibility"),
		//index.Fields("checksum"),
	}
}
