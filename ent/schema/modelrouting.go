package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// ModelRouting is a singleton configuration row for model selection and fallback.
type ModelRouting struct {
	ent.Schema
}

func (ModelRouting) Fields() []ent.Field {
	return []ent.Field{
		field.String("config_key").Unique().Immutable().NotEmpty(),
		field.String("default_chat_model").Default(""),
		field.String("default_embedding_model").Default(""),
		field.JSON("fallback_chain", []string{}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (ModelRouting) Edges() []ent.Edge { return nil }
