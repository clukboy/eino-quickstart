package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AgentDef stores the editable definition of a console Agent.
type AgentDef struct {
	ent.Schema
}

func (AgentDef) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),
		field.Text("description").Default(""),
		field.String("owner_subject").NotEmpty(),
		field.Enum("status").Values("DRAFT", "PUBLISHED", "ARCHIVED").Default("DRAFT"),
		field.Int("current_version").Default(0),
		field.Int("published_version").Default(0),
		field.String("model").Default(""),
		field.String("risk_tier").Default("T1"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (AgentDef) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("versions", AgentVersion.Type),
		edge.To("datasets", AgentDefDataset.Type),
	}
}

func (AgentDef) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status"),
		index.Fields("owner_subject", "status"),
	}
}
