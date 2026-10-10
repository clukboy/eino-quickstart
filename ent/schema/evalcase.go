package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// EvalCase is one question/expected-answer pair in an evaluation set.
type EvalCase struct {
	ent.Schema
}

func (EvalCase) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("eval_set_id"),
		field.Text("question").NotEmpty(),
		field.Text("expected_answer").Default(""),
		field.JSON("tags", []string{}),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (EvalCase) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("eval_set", EvalSet.Type).
			Ref("cases").
			Field("eval_set_id").
			Unique().
			Required(),
		edge.To("results", EvalCaseResult.Type),
	}
}

func (EvalCase) Indexes() []ent.Index {
	return []ent.Index{index.Fields("eval_set_id", "created_at")}
}
