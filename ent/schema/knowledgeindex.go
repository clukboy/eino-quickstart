package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// KnowledgeIndex holds the schema definition for the KnowledgeIndex entity.
type KnowledgeIndex struct {
	ent.Schema
}

// Fields of the KnowledgeIndex.
func (KnowledgeIndex) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").Unique().Immutable(),
		field.String("milvus_collection").Unique(),
		field.String("embedding_model"),
		field.Int("embedding_dimensions"),
		field.Enum("status").Values("BUILDING", "ACTIVE", "RETIRED", "FAILED").Default("BUILDING"),
		field.Time("activated_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

// Edges of the KnowledgeIndex.
func (KnowledgeIndex) Edges() []ent.Edge {
	return nil
}
