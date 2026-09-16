package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Dataset holds the schema definition for the Dataset entity.
type Dataset struct {
	ent.Schema
}

// Fields of the Dataset.
func (Dataset) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").Unique(),
		field.String("description").Optional(),
		field.String("owner_subject").Default("system"),
		field.Enum("visibility").Values("system", "private").Default("system"),
		field.Enum("status").Values("ACTIVE", "DISABLED").Default("ACTIVE"),
		field.String("type").Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Edges of the Dataset.
func (Dataset) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("folders", KnowledgeFolder.Type),
		edge.To("documents", Document.Type),
		edge.To("agent_datasets", AgentDataset.Type),
	}
}
