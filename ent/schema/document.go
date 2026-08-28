package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Document holds the schema definition for the Document entity.
type Document struct {
	ent.Schema
}

// Fields of the Document.
func (Document) Fields() []ent.Field {
	return []ent.Field{
		field.String("source").Unique().Immutable(),
		field.String("title"),
		field.String("checksum"),
		field.String("owner_subject").Default("system"),
		field.Enum("visibility").
			Values("system", "private").
			Default("system"),
		field.Enum("status").
			Values("ready", "indexing", "failed", "deleted").
			Default("indexing"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Edges of the Document.
func (Document) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("chunks", DocumentChunk.Type),
	}
}

func (Document) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("owner_subject", "visibility"),
		index.Fields("checksum"),
	}
}
