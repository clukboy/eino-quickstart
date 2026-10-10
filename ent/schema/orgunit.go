package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OrgUnit stores a node in the organization tree.
type OrgUnit struct {
	ent.Schema
}

func (OrgUnit) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),
		field.Uint64("parent_id").Optional().Nillable(),
		field.String("path").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (OrgUnit) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("parent", OrgUnit.Type).
			Ref("children").
			Field("parent_id").
			Unique(),
		edge.To("children", OrgUnit.Type),
		edge.To("members", OrgMember.Type),
	}
}

func (OrgUnit) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("parent_id"),
		index.Fields("path").Unique(),
	}
}
