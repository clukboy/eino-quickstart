package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OrgMember records subject membership. A subject may be external, so it is
// intentionally not a foreign key to User.
type OrgMember struct {
	ent.Schema
}

func (OrgMember) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("org_unit_id"),
		field.String("subject").NotEmpty(),
		field.Time("joined_at").Default(time.Now).Immutable(),
	}
}

func (OrgMember) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("org_unit", OrgUnit.Type).
			Ref("members").
			Field("org_unit_id").
			Unique().
			Required(),
	}
}

func (OrgMember) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("org_unit_id", "subject").Unique(),
		index.Fields("subject"),
	}
}
