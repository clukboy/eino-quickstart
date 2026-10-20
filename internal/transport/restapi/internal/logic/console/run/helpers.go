package run

import (
	"eino-quickstart/ent"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/types"
)

func fail(err error) error {
	if ent.IsNotFound(err) {
		return httpx.NotFound("run not found")
	}
	return httpx.Internal("run operation failed")
}

// turnDTO 把一轮对话映射成控制台「运行记录」的一行。
//
// 字段口径：run_id 就是 turn_id（两个概念已经合并成一张表）；steps / tool_calls /
// tokens / cost / latency 保持 0 —— 这一版没有采集，宁可留空也不编数字。
// agent_key 恒为空：只有一个 agent，按 key 分组已经没有意义。
func turnDTO(turn *ent.ChatTurn) *types.RunResp {
	resp := &types.RunResp{
		RunID:     turn.TurnID,
		SessionID: turn.SessionID,
		Subject:   turn.OwnerSubject,
		Status:    string(turn.Status),
		StartedAt: turn.CreatedAt.UnixMilli(),
	}
	if turn.CompletedAt != nil {
		resp.FinishedAt = turn.CompletedAt.UnixMilli()
	}

	return resp
}
