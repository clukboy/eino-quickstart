package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Model stores a model exposed by a configured provider.
type Model struct {
	ent.Schema
}

func (Model) Fields() []ent.Field {
	return []ent.Field{
		field.String("key").Unique().Immutable().NotEmpty(),
		field.Uint64("provider_id"),
		field.String("provider_key").NotEmpty(),
		field.Enum("kind").Values("chat", "embedding", "rerank", "image"),
		field.Int("context_window").Default(0),
		field.Int("max_output_tokens").Default(0),
		field.Int64("input_price_micros_per_k").Default(0),
		field.Int64("output_price_micros_per_k").Default(0),
		field.Bool("enabled").Default(true),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Model) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("provider", ModelProvider.Type).
			Ref("models").
			Field("provider_id").
			Unique().
			Required(),
	}
}

func (Model) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("provider_id", "kind"),
		index.Fields("provider_key", "kind"),
		index.Fields("enabled", "kind"),
	}
}
