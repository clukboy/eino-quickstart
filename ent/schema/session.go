package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Session holds the schema definition for the Session entity.
type Session struct {
	ent.Schema
}

// Fields of the Session.
func (Session) Fields() []ent.Field {
	return []ent.Field{
		field.String("session_id").Unique().Immutable().NotEmpty(),
		field.String("owner_subject").Immutable(),
		// Title 是会话在侧栏里的显示名，由首条用户消息派生
		// （internal/platform/persistence/session 的 DeriveTitle）。
		// 空串表示「这条会话还没有人说过话」，界面显示「新对话」；
		// 所以它不能是 NotEmpty，也不能是 NULL —— 空串是一个有意义的状态。
		field.String("title").Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
		// UpdatedAt 是会话的最后活动时间：会话列表按它倒序，
		// 也是「这条会话最近还能不能接着聊」的依据。
		//
		// UpdateDefault 让任何一次 Session 行的 Update 自动刷新它；
		// 而 DefaultExpr 是**给数据库 DDL 用的**：只写 field.Default 的话 ent
		// 不会把它落到建表语句里（对照 ent/migrate/schema.go 的 created_at，
		// 那里没有 Default 字段），于是给一张已存在的表加这一列会变成
		// ALTER TABLE ... ADD COLUMN updated_at timestamptz NOT NULL（没有默认值），
		// PostgreSQL 直接报 "contains null values"，启动即迁移失败。
		// 有了这个注解，DDL 才是 ... NOT NULL DEFAULT now()。
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			Annotations(entsql.DefaultExpr("now()")),
	}
}

// Edges of the Session.
func (Session) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("messages", SessionMessage.Type),
	}
}

// Indexes of the Session.
func (Session) Indexes() []ent.Index {
	return []ent.Index{
		// 会话列表的查询就是 WHERE owner_subject = ? ORDER BY updated_at DESC，
		// 两列都要、且顺序就是这个。
		index.Fields("owner_subject", "updated_at"),
	}
}

func (Session) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("sessions"),
		schema.Comment("Session represents a user session in the system."),
	}
}
