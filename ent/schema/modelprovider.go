package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ModelProvider stores connection metadata for an external model provider.
type ModelProvider struct {
	ent.Schema
}

func (ModelProvider) Fields() []ent.Field {
	return []ent.Field{
		field.String("key").Unique().Immutable().NotEmpty(),
		field.String("name").NotEmpty(),
		field.String("base_url").Default(""),
		field.Enum("auth_type").Values("none", "bearer", "api_key", "oauth2").Default("none"),
		field.Text("secret_ciphertext").Optional().Nillable(),
		field.Bool("enabled").Default(true),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (ModelProvider) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("models", Model.Type),
	}
}

func (ModelProvider) Indexes() []ent.Index {
	return []ent.Index{index.Fields("enabled")}
}
