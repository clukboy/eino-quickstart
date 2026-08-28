package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ChatTurn holds the schema definition for the ChatTurn entity.
type ChatTurn struct {
	ent.Schema
}

// Fields of the ChatTurn.
func (ChatTurn) Fields() []ent.Field {
	return []ent.Field{
		field.String("turn_id").Unique().Immutable(),
		field.String("session_id").Immutable(),
		field.String("owner_subject").Immutable(),
		field.Text("user_content").Immutable(),
		field.Text("assistant_content").Optional().Nillable(),
		field.Enum("status").Values("RUNNING", "INTERRUPTED", "COMPLETED", "FAILED").Default("RUNNING"),
		field.String("approval_id").Optional().Nillable(),
		field.String("checkpoint_id").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("completed_at").Optional().Nillable(),
	}
}

// Edges of the ChatTurn.
func (ChatTurn) Edges() []ent.Edge {
	return nil
}

func (ChatTurn) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "created_at"),
		index.Fields("owner_subject", "created_at"),
		index.Fields("approval_id").Unique(),
		index.Fields("status", "completed_at"),
	}
}
