package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// PasswordPolicy is a singleton security configuration row.
type PasswordPolicy struct {
	ent.Schema
}

func (PasswordPolicy) Fields() []ent.Field {
	return []ent.Field{
		field.String("config_key").Unique().Immutable().NotEmpty(),
		field.Int("min_length").Default(12),
		field.Bool("require_upper").Default(true),
		field.Bool("require_lower").Default(true),
		field.Bool("require_digit").Default(true),
		field.Bool("require_symbol").Default(false),
		field.Int("max_age_days").Default(0),
		field.Int("history_count").Default(5),
		field.Int("max_failed_attempts").Default(5),
		field.Int("lockout_seconds").Default(900),
		field.Int("session_ttl_hours").Default(24),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (PasswordPolicy) Edges() []ent.Edge { return nil }
