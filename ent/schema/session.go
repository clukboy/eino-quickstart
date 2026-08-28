package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Session holds the schema definition for the Session entity.
type Session struct {
	ent.Schema
}

// Fields of the Session.
func (Session) Fields() []ent.Field {
	return []ent.Field{
		field.String("session_id").Unique().Immutable().NotEmpty(),
		field.String("owner_subject").Immutable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

// Edges of the Session.
func (Session) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("messages", SessionMessage.Type),
	}
}

func (Session) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("sessions"),
		schema.Comment("Session represents a user session in the system."),
	}
}
