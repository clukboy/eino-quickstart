package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AgentVersion is an immutable, versioned snapshot of an Agent definition.
type AgentVersion struct {
	ent.Schema
}

func (AgentVersion) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("agent_def_id"),
		field.Int("version"),
		field.Text("system_prompt").Default(""),
		field.String("model").Default(""),
		field.Float("temperature").Default(0.7),
		field.Int("max_iterations").Default(6),
		field.JSON("tool_keys", []string{}),
		field.JSON("dataset_ids", []uint64{}),
		field.Enum("status").Values("DRAFT", "PUBLISHED", "RETIRED").Default("DRAFT"),
		field.Text("change_note").Optional().Nillable(),
		field.String("created_by").NotEmpty(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (AgentVersion) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("agent_def", AgentDef.Type).
			Ref("versions").
			Field("agent_def_id").
			Unique().
			Required(),
	}
}

func (AgentVersion) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("agent_def_id", "version").Unique(),
		index.Fields("agent_def_id", "status"),
	}
}
