package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Approval holds the schema definition for the Approval entity.
type Approval struct {
	ent.Schema
}

// Fields of the Approval.
func (Approval) Fields() []ent.Field {
	return []ent.Field{
		field.String("approval_id").Unique().Immutable(),
		field.String("session_id").Immutable(),
		field.String("run_id").Optional().Nillable(),
		field.String("turn_id").Optional().Nillable(),
		field.String("tool").Immutable(),
		field.String("arguments_hash").Optional().Nillable(),
		field.Text("display_arguments").Optional().Nillable(),
		field.Enum("status").Values("PENDING", "APPROVED", "REJECTED", "RESUMING", "EXECUTED", "EXPIRED").Default("PENDING"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.String("requested_by").Immutable(),
		field.Time("decided_at").Optional().Nillable(),
		field.Time("executed_at").Optional().Nillable(),
		field.String("decided_by").Optional().Nillable(),
		field.String("checkpoint_id").Optional().Nillable(),
		field.String("interrupt_id").Optional().Nillable(),
		field.Time("expires_at").Optional().Nillable(),
	}
}

// Edges of the Approval.
func (Approval) Edges() []ent.Edge {
	return nil
}

func (Approval) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status", "expires_at"),
		index.Fields("checkpoint_id"),
	}
}
