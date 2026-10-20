package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ChatTurn holds the schema definition for the ChatTurn entity.
type ChatTurn struct {
	ent.Schema
}

// Fields of the ChatTurn.
//
// 这张表是**一轮对话的执行记录**：谁问的、问了什么、答了什么、成没成。
// 它同时是控制台「运行记录」的唯一数据源 —— 早先那张 agent_runs 与它逐列重复
// （同样的 session_id / user_message / status / completed_at），还带着审批时代
// 的 checkpoint_id / approval_id / RESUMING / EXPIRED 和一批从未写入的统计列，
// 已经删掉。
//
// 与 session_messages 的分工：本表按轮次配对、会被 maintenance 按保留期清理；
// session_messages 是长期历史，不过期。
func (ChatTurn) Fields() []ent.Field {
	return []ent.Field{
		field.String("turn_id").Unique().Immutable(),
		field.String("session_id").Immutable(),
		field.String("owner_subject").Immutable(),
		field.Text("user_content").Immutable(),
		field.Text("assistant_content").Optional().Nillable(),
		// INTERRUPTED 仍在枚举里：历史数据里可能有，删枚举值会让存量行读不出来。
		// 新写入只会产生 RUNNING / COMPLETED / FAILED 三种。
		field.Enum("status").Values("RUNNING", "INTERRUPTED", "COMPLETED", "FAILED").Default("RUNNING"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("completed_at").Optional().Nillable(),
	}
}

// Edges of the ChatTurn.
func (ChatTurn) Edges() []ent.Edge {
	return nil
}

func (ChatTurn) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "created_at"),
		index.Fields("owner_subject", "created_at"),
		index.Fields("status", "completed_at"),
	}
}
