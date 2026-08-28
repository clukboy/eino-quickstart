package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// DocumentChunk holds the schema definition for the DocumentChunk entity.
type DocumentChunk struct {
	ent.Schema
}

// Fields of the DocumentChunk.
func (DocumentChunk) Fields() []ent.Field {
	return []ent.Field{
		field.Int("chunk_index").Immutable(),
		field.String("citation_id").Unique().Immutable(),
		field.Text("content"),
		field.String("heading_path").Optional().Nillable(),
		field.Int("start_line"),
		field.Int("end_line"),
		field.Int("character_count"),
		field.String("embedding_model"),
		field.Enum("vector_status").
			Values("pending", "indexed", "failed", "deleting").
			Default("pending"),
		field.Time("indexed_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

// Edges of the DocumentChunk.
func (DocumentChunk) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("document", Document.Type).
			Ref("chunks").
			Unique().
			Required(),
	}
}

func (DocumentChunk) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("chunk_index").Edges("document").Unique(),
		index.Fields("vector_status"),
	}
}
