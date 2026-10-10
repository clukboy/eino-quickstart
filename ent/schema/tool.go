package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Tool stores a configured builtin, HTTP, MCP, or OpenAPI tool.
type Tool struct {
	ent.Schema
}

func (Tool) Fields() []ent.Field {
	return []ent.Field{
		field.String("key").Unique().Immutable().NotEmpty(),
		field.String("name").NotEmpty(),
		field.Enum("kind").Values("builtin", "http", "mcp", "openapi"),
		field.Text("description").Default(""),
		field.String("risk_tier").Default("T1"),
		field.Bool("enabled").Default(true),
		field.Text("config_schema").Optional().Nillable(),
		field.Enum("auth_type").Values("none", "bearer", "api_key", "oauth2").Default("none"),
		// Only encrypted/managed secret material belongs here. API responses must
		// expose a derived has_secret flag instead of this value.
		field.Text("secret_ciphertext").Optional().Nillable(),
		field.String("endpoint").Optional().Nillable(),
		field.Int("timeout_ms").Default(30000),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Tool) Edges() []ent.Edge { return nil }

func (Tool) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("kind", "enabled"),
		index.Fields("risk_tier", "enabled"),
	}
}
