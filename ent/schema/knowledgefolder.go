package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// KnowledgeFolder holds the schema definition for the KnowledgeFolder entity.
type KnowledgeFolder struct {
	ent.Schema
}

// Fields of the KnowledgeFolder.
func (KnowledgeFolder) Fields() []ent.Field {
	return []ent.Field{
		field.String("name"),
		field.String("path"),
		field.Int("sort").Default(0),
		field.Int("knowledge_base_id").Immutable(),

		field.Int("parent_id").Optional().Nillable(),

		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Edges of the KnowledgeFolder.
func (KnowledgeFolder) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("knowledge_base", KnowledgeBase.Type).Ref("folders").Unique().Required(),
		edge.From("parent", KnowledgeFolder.Type).Field("parent_id").Ref("children").Unique(),
		edge.To("children", KnowledgeFolder.Type),
		edge.To("documents", Document.Type),
	}
}

func (KnowledgeFolder) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("knowledge_base_id", "parent_id"),
		index.Fields("knowledge_base_id", "path").Unique(),
	}
}
