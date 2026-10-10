package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AuditEvent holds the schema definition for the AuditEvent entity.
type AuditEvent struct {
	ent.Schema
}

// Fields of the AuditEvent.
func (AuditEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("session_id"),
		field.String("approval_id").Optional(),
		field.String("event_type"),
		field.String("tool").Optional(),
		field.Text("payload"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.String("actor_subject").Optional().Nillable(),
		// Console audit fields are optional to preserve compatibility with the
		// existing execution-event records stored by the runtime.
		field.String("action").Optional().Nillable(),
		field.String("resource_type").Optional().Nillable(),
		field.String("resource_id").Optional().Nillable(),
		field.String("resource_name").Optional().Nillable(),
		field.Text("summary").Optional().Nillable(),
		field.JSON("before", map[string]any{}).Optional(),
		field.JSON("after", map[string]any{}).Optional(),
		field.String("ip").Optional().Nillable(),
	}
}

// Edges of the AuditEvent.
func (AuditEvent) Edges() []ent.Edge { return nil }

func (AuditEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("actor_subject", "created_at"),
		index.Fields("action", "created_at"),
		index.Fields("resource_type", "resource_id"),
	}
}
