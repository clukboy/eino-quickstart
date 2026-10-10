package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Feedback stores user feedback and its moderation/triage state.
type Feedback struct {
	ent.Schema
}

func (Feedback) Fields() []ent.Field {
	return []ent.Field{
		field.String("run_id").NotEmpty(),
		field.String("session_id").Optional().Nillable(),
		field.String("subject").NotEmpty(),
		field.String("rating").NotEmpty(),
		field.Text("comment").Default(""),
		field.String("label").Default("PENDING"),
		field.Text("note").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Feedback) Edges() []ent.Edge { return nil }

func (Feedback) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("label", "created_at"),
		index.Fields("subject", "created_at"),
		index.Fields("run_id"),
	}
}
