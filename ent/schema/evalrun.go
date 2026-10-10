package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// EvalRun stores one asynchronous evaluation execution.
type EvalRun struct {
	ent.Schema
}

func (EvalRun) Fields() []ent.Field {
	return []ent.Field{
		field.String("run_id").Unique().Immutable().NotEmpty(),
		field.Uint64("eval_set_id"),
		field.Enum("status").Values("RUNNING", "COMPLETED", "FAILED").Default("RUNNING"),
		field.Int("total").Default(0),
		field.Int("completed").Default(0),
		field.Int("passed").Default(0),
		field.Float("score").Default(0),
		field.Time("started_at").Default(time.Now).Immutable(),
		field.Time("finished_at").Optional().Nillable(),
		field.Text("error").Optional().Nillable(),
	}
}

func (EvalRun) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("eval_set", EvalSet.Type).
			Ref("runs").
			Field("eval_set_id").
			Unique().
			Required(),
		edge.To("results", EvalCaseResult.Type),
	}
}

func (EvalRun) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("eval_set_id", "started_at"),
		index.Fields("status"),
	}
}
