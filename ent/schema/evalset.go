package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// EvalSet is a collection of evaluation cases for an Agent.
type EvalSet struct {
	ent.Schema
}

func (EvalSet) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),
		field.Text("description").Default(""),
		field.String("agent_key").Optional().Nillable(),
		field.String("created_by").NotEmpty(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (EvalSet) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("cases", EvalCase.Type),
		edge.To("runs", EvalRun.Type),
	}
}
