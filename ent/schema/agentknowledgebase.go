package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AgentKnowledgeBase grants one authenticated agent subject access to one knowledge base.
type AgentKnowledgeBase struct {
	ent.Schema
}

func (AgentKnowledgeBase) Fields() []ent.Field {
	return []ent.Field{
		field.String("subject").Immutable(),
		field.Uint64("knowledge_base_id"),
		field.String("created_by").Immutable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (AgentKnowledgeBase) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("knowledge_base", KnowledgeBase.Type).
			Ref("agent_knowledge_bindings").
			Field("knowledge_base_id").
			Unique().
			Required(),
	}
}

func (AgentKnowledgeBase) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("subject", "knowledge_base_id").Unique(),
	}
}
