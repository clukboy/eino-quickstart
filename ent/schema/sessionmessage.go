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

// SessionMessage holds the schema definition for the SessionMessage entity.
type SessionMessage struct {
	ent.Schema
}

// Fields of the SessionMessage.
func (SessionMessage) Fields() []ent.Field {
	return []ent.Field{
		field.String("role").NotEmpty(),
		field.String("content").NotEmpty(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

// Edges of the SessionMessage.
func (SessionMessage) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("session", Session.Type).Ref("messages").Unique().Required(),
	}
}

// Indexes of the SessionMessage.
func (SessionMessage) Indexes() []ent.Index {
	return []ent.Index{
		// 会话历史（session.Store.History / Messages）是按 session 查的，
		// 而这张表此前**一个索引都没有**，每次对话都在全表扫描。
		//
		// 为什么只能用 Edges 引、不能写成
		// index.Fields("created_at").Edges("session")：Fields 与 Edges 混用时
		// 生成的列顺序是「fields 在前、edge 外键在后」—— entc/gen/type.go 的
		// buildIndexes 先遍历一遍 idx.Fields、再遍历一遍 idx.Edges，所以两者
		// 的书写先后无关。实测 documentchunk 的
		// index.Fields("chunk_index").Edges("document") 生成的是
		// {chunk_index, document_chunks}，leading column 就不是 session 了，
		// 对 WHERE session 的查询等于白建。
		//
		// 外键列本身没有对应的 Go 侧字段，所以这里只能按边引用；而且 Edges
		// 只认**反向**边（edge.From），指到 edge.To 上会生成失败。
		//
		// 它的列名是 session_messages（与表同名）—— 那是 ent 的默认命名，规则是
		// `<所属类型的 label>_<正向边名>`（entc/gen/graph.go 的 resolve()：
		// fmt.Sprintf("%s_%s", e.Type.Label(), snake(ref.Name))，这么取是为了
		// 「反向边增删都不改列名」）。本例：所属类型 Session → "session"，
		// Session 的正向边 edge.To("messages") → "messages"，拼出 session_messages。
		// 见 ent/migrate/schema.go 的 SessionMessagesColumns 与
		// ent/sessionmessage/sessionmessage.go 的 SessionColumn 常量。
		// 想正名成 session_id 需要先手工 RENAME COLUMN 再改 schema，
		// 否则 WithDropColumn 会把旧列 DROP 掉、存量行的关联全断。
		index.Edges("session"),
	}
}

func (SessionMessage) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("session_messages"),
		schema.Comment("SessionMessage represents a message in a user session."),
	}
}
