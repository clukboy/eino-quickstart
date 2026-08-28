package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// SessionMessage holds the schema definition for the SessionMessage entity.
type SessionMessage struct {
	ent.Schema
}

// Fields of the SessionMessage.
func (SessionMessage) Fields() []ent.Field {
	return []ent.Field{
		field.String("role").NotEmpty(),
		field.String("content").NotEmpty(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

// Edges of the SessionMessage.
func (SessionMessage) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("session", Session.Type).Ref("messages").Unique().Required(),
	}
}

func (SessionMessage) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("session_messages"),
		schema.Comment("SessionMessage represents a message in a user session."),
	}
}
