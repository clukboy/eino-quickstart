package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// RunSpan stores one step in an Agent run trace.
type RunSpan struct {
	ent.Schema
}

func (RunSpan) Fields() []ent.Field {
	return []ent.Field{
		field.String("run_id").NotEmpty(),
		field.String("span_id").Unique().Immutable().NotEmpty(),
		field.String("parent_id").Optional().Nillable(),
		field.String("kind").NotEmpty(),
		field.String("name").NotEmpty(),
		field.String("status").NotEmpty(),
		field.Int64("latency_ms").Default(0),
		field.Int64("tokens_in").Default(0),
		field.Int64("tokens_out").Default(0),
		field.Text("input").Optional().Nillable(),
		field.Text("output").Optional().Nillable(),
		field.Bool("input_truncated").Default(false),
		field.Bool("output_truncated").Default(false),
		field.Text("error").Optional().Nillable(),
		field.Time("started_at").Default(time.Now).Immutable(),
		field.Time("finished_at").Optional().Nillable(),
	}
}

func (RunSpan) Edges() []ent.Edge { return nil }

func (RunSpan) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("run_id", "started_at"),
		index.Fields("parent_id"),
	}
}
