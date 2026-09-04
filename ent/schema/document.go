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
		field.JSON("metadata", map[string]any{}).Optional(),
		field.String("checksum"),
		field.String("owner_subject").Default("system"),
		field.Enum("visibility").
			Values("system", "private").
			Default("system"),
		field.Enum("status").Values("ready", "indexing", "failed", "deleted").Default("indexing"),

		field.Int("knowledge_base_id"),
		field.Int("folder_id").Optional().Nillable(),

		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Edges of the Document.
func (Document) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("knowledge_base", KnowledgeBase.Type).
			Ref("documents").
			Field("knowledge_base_id").
			Unique().
			Required(),

		edge.From("folder", KnowledgeFolder.Type).
			Ref("documents").
			Field("folder_id").
			Unique(),

		edge.To("chunks", DocumentChunk.Type),
	}
}

func (Document) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("knowledge_base_id", "source").Unique(),
		index.Fields("owner_subject", "visibility"),
		index.Fields("checksum"),
	}
}
