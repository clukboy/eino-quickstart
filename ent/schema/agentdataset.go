package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AgentDataset grants one authenticated agent subject access to one dataset.
type AgentDataset struct {
	ent.Schema
}

func (AgentDataset) Fields() []ent.Field {
	return []ent.Field{
		field.String("subject").Immutable(),
		field.Uint64("dataset_id"),
		field.String("created_by").Immutable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (AgentDataset) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("dataset", Dataset.Type).
			Ref("agent_datasets").
			Field("dataset_id").
			Unique().
			Required(),
	}
}

func (AgentDataset) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("subject", "dataset_id").Unique(),
	}
}
