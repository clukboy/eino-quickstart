package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Checkpoint holds the schema definition for the Checkpoint entity.
type Checkpoint struct {
	ent.Schema
}

// Fields of the Checkpoint.
func (Checkpoint) Fields() []ent.Field {
	return []ent.Field{
		field.String("checkpoint_id").
			Unique().
			Immutable(),
		field.Bytes("payload"),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
		field.Time("expires_at").
			Optional().
			Nillable(),
	}
}

// Edges of the Checkpoint.
func (Checkpoint) Edges() []ent.Edge {
	return nil
}
