package overview

import (
	"context"
	"time"

	"eino-quickstart/ent"
	"eino-quickstart/ent/chatturn"
	"eino-quickstart/ent/session"
	"eino-quickstart/ent/sessionmessage"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetOverviewLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetOverviewLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetOverviewLogic {
	return &GetOverviewLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

// GetOverview 汇总最近 N 天的用量。
//
// 数据源是 chat_turns —— 一轮对话一行。早先这里读的是 agent_runs，那张表与
// chat_turns 完全重复且从未写入过 token/耗时/成本，所以数字恒为 0；现在直接
// 用轮次计数，失败轮次也来自同一处，控制的「运行记录」页面同样改读它。
//
// 契约里的字段名保持 `runs` / `failed_runs` 不变（前端在用），语义即「轮次」。
// TokensIn/TokensOut/CostMicros 恒为 0：这一版没有采集用量，不编造数字。
func (l *GetOverviewLogic) GetOverview(req *types.OverviewReq) (*types.OverviewResp, error) {
	days := req.Days
	if days <= 0 {
		days = 7
	}
	from := time.Now().Add(-time.Duration(days) * 24 * time.Hour)

	turns, err := l.svcCtx.EntClient.ChatTurn.
		Query().
		Where(chatturn.CreatedAtGTE(from)).
		All(l.ctx)
	if err != nil {
		return nil, err
	}

	kpi := &types.OverviewKpiResp{}
	fails := make([]*types.RunResp, 0)
	for _, turn := range turns {
		kpi.Runs++
		if turn.Status != chatturn.StatusFAILED {
			continue
		}
		kpi.FailedRuns++
		if len(fails) < 20 {
			fails = append(fails, turnDTO(turn))
		}
	}

	if sessions, err := l.svcCtx.EntClient.Session.
		Query().
		Where(session.CreatedAtGTE(from)).
		Count(l.ctx); err == nil {
		kpi.Sessions = sessions
	}
	if messages, err := l.svcCtx.EntClient.SessionMessage.
		Query().
		Where(sessionmessage.CreatedAtGTE(from)).
		Count(l.ctx); err == nil {
		kpi.Messages = messages
	}

	return &types.OverviewResp{
		Kpi:         kpi,
		Trend:       make([]*types.OverviewPointResp, 0),
		TopAgents:   make([]*types.OverviewTopResp, 0),
		TopDatasets: make([]*types.OverviewTopResp, 0),
		RecentFails: fails,
	}, nil
}

// turnDTO 把一轮对话映射成控制台「运行记录」的一行。
//
// run_id 字段里放的其实是 turn_id —— 响应形状保持不变是为了不动前端契约；
// 两个概念合并之后，一轮对话就是一次运行。
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
