package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// VectorOutbox holds the schema definition for the VectorOutbox entity.
type VectorOutbox struct {
	ent.Schema
}

// Fields of the VectorOutbox.
func (VectorOutbox) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("chunk_id").Unique(),
		field.Enum("operation").
			Values("upsert", "delete"),
		field.Enum("status").
			Values("pending", "processing", "done", "failed").
			Default("pending"),
		field.Int("attempts").Default(0),
		field.Time("available_at").Default(time.Now),
		field.Time("locked_until").Optional().Nillable(),
		field.String("last_error").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Edges of the VectorOutbox.
func (VectorOutbox) Edges() []ent.Edge {
	return nil
}

func (VectorOutbox) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status", "available_at"),
	}
}
