package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ApprovalPolicy defines the approval behavior for a tool and risk tier.
type ApprovalPolicy struct {
	ent.Schema
}

func (ApprovalPolicy) Fields() []ent.Field {
	return []ent.Field{
		field.String("tool_key").NotEmpty(),
		field.String("risk_tier").NotEmpty(),
		field.Enum("mode").Values("auto", "notify", "inline", "multi"),
		field.JSON("approver_roles", []string{}),
		field.Int("quorum").Default(1),
		field.Int("timeout_seconds").Default(0),
		field.String("on_timeout").Default("reject"),
		field.Bool("enabled").Default(true),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (ApprovalPolicy) Edges() []ent.Edge { return nil }

func (ApprovalPolicy) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tool_key", "risk_tier").Unique(),
		index.Fields("enabled"),
	}
}
