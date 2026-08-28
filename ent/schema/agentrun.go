package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AgentRun holds the schema definition for the AgentRun entity.
type AgentRun struct {
	ent.Schema
}

// Fields of the AgentRun.
func (AgentRun) Fields() []ent.Field {
	return []ent.Field{
		field.String("run_id").Unique().Immutable(),
		field.String("session_id").Immutable(),
		field.String("requested_by").Immutable(),
		field.Text("user_message").Immutable(),
		field.String("checkpoint_id").Optional().Nillable().Immutable(),
		field.Enum("status").Values("RUNNING", "INTERRUPTED", "RESUMING", "COMPLETED", "FAILED", "EXPIRED").Default("RUNNING"),
		field.String("approval_id").Optional().Nillable(),
		field.String("error_code").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
		field.Time("completed_at").Optional().Nillable(),
		field.Time("expires_at").Optional().Nillable(),
	}
}

// Edges of the AgentRun.
func (AgentRun) Edges() []ent.Edge {
	return nil
}

func (AgentRun) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "status"),
		index.Fields("checkpoint_id"),
		index.Fields("expires_at"),
	}
}
