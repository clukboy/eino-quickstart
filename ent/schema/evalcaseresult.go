package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// EvalCaseResult is the persisted result of one case in one evaluation run.
type EvalCaseResult struct {
	ent.Schema
}

func (EvalCaseResult) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("eval_run_id"),
		field.Uint64("case_id"),
		field.Text("question").Default(""),
		field.Text("answer").Default(""),
		field.Float("score").Default(0),
		field.Bool("passed").Default(false),
		field.Text("reason").Optional().Nillable(),
		field.Int64("latency_ms").Default(0),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (EvalCaseResult) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("eval_run", EvalRun.Type).
			Ref("results").
			Field("eval_run_id").
			Unique().
			Required(),
		edge.From("eval_case", EvalCase.Type).
			Ref("results").
			Field("case_id").
			Unique().
			Required(),
	}
}

func (EvalCaseResult) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("eval_run_id", "case_id").Unique(),
		index.Fields("case_id"),
	}
}
