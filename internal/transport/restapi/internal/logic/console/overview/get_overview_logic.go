package overview

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/ent/agentrun"
	"eino-quickstart/ent/session"
	"eino-quickstart/ent/sessionmessage"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
	"time"
)

type GetOverviewLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetOverviewLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetOverviewLogic {
	return &GetOverviewLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *GetOverviewLogic) GetOverview(req *types.OverviewReq) (*types.OverviewResp, error) {
	days := req.Days
	if days <= 0 {
		days = 7
	}
	from := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	runs, e := l.svcCtx.EntClient.AgentRun.Query().Where(agentrun.CreatedAtGTE(from)).All(l.ctx)
	if e != nil {
		return nil, e
	}
	k := &types.OverviewKpiResp{}
	fails := make([]*types.RunResp, 0)
	for _, r := range runs {
		k.Runs++
		k.TokensIn += r.TokensIn
		k.TokensOut += r.TokensOut
		k.CostMicros += r.CostMicros
		if r.Status == agentrun.StatusFAILED {
			k.FailedRuns++
			if len(fails) < 20 {
				fails = append(fails, runDTO(r))
			}
		}
	}
	sessions, e := l.svcCtx.EntClient.Session.Query().Where(session.CreatedAtGTE(from)).Count(l.ctx)
	if e == nil {
		k.Sessions = sessions
	}
	messages, e := l.svcCtx.EntClient.SessionMessage.Query().Where(sessionmessage.CreatedAtGTE(from)).Count(l.ctx)
	if e == nil {
		k.Messages = messages
	}
	return &types.OverviewResp{Kpi: k, Trend: make([]*types.OverviewPointResp, 0), TopAgents: make([]*types.OverviewTopResp, 0), TopDatasets: make([]*types.OverviewTopResp, 0), RecentFails: fails}, nil /* TODO: add bucketed trend, cost aggregation, and pending approval counters. */
}
func runDTO(r *ent.AgentRun) *types.RunResp {
	return &types.RunResp{RunID: r.RunID, SessionID: r.SessionID, AgentKey: r.AgentKey, AgentVersion: r.AgentVersion, Subject: r.RequestedBy, Status: string(r.Status), Steps: r.Steps, ToolCalls: r.ToolCalls, TokensIn: r.TokensIn, TokensOut: r.TokensOut, LatencyMS: r.LatencyMs, CostMicros: r.CostMicros, StartedAt: r.CreatedAt.UnixMilli()}
}
