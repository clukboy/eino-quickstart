package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
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
	}
}

// Edges of the AuditEvent.
func (AuditEvent) Edges() []ent.Edge {
	return nil
}
