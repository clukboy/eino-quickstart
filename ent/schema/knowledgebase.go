package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// KnowledgeBase holds the schema definition for the KnowledgeBase entity.
type KnowledgeBase struct {
	ent.Schema
}

// Fields of the KnowledgeBase.
func (KnowledgeBase) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").Unique(),
		field.String("description").Optional(),
		field.String("owner_subject").Default("system"),
		field.Enum("visibility").Values("system", "private").Default("system"),
		field.Enum("status").Values("ACTIVE", "DISABLED").Default("ACTIVE"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Edges of the KnowledgeBase.
func (KnowledgeBase) Edges() []ent.Edge {
	return []ent.Edge{edge.To(
		"folders", KnowledgeFolder.Type),
		edge.To("documents", Document.Type),
	}
}
