package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AgentDefDataset binds an Agent definition to a dataset for retrieval.
type AgentDefDataset struct {
	ent.Schema
}

func (AgentDefDataset) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("agent_def_id"),
		field.Uint64("dataset_id"),
		field.String("created_by").NotEmpty(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (AgentDefDataset) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("agent_def", AgentDef.Type).
			Ref("datasets").
			Field("agent_def_id").
			Unique().
			Required(),
		edge.From("dataset", Dataset.Type).
			Ref("agent_def_datasets").
			Field("dataset_id").
			Unique().
			Required(),
	}
}

func (AgentDefDataset) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("agent_def_id", "dataset_id").Unique(),
		index.Fields("dataset_id"),
	}
}
