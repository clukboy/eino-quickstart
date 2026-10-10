package run

import (
	"context"
	"eino-quickstart/ent/agentrun"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
	"time"
)

type ListRunsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListRunsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListRunsLogic {
	return &ListRunsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *ListRunsLogic) ListRuns(req *types.RunListReq) (*types.RunListResp, error) {
	p, s := req.Page, req.PageSize
	if p <= 0 {
		p = 1
	}
	if s <= 0 || s > 200 {
		s = 20
	}
	q := l.svcCtx.EntClient.AgentRun.Query()
	if req.Subject != "" {
		q.Where(agentrun.RequestedByEQ(req.Subject))
	}
	if req.AgentKey != "" {
		q.Where(agentrun.AgentKeyEQ(req.AgentKey))
	}
	if req.Status != "" {
		q.Where(agentrun.StatusEQ(agentrun.Status(req.Status)))
	}
	if req.FromMS > 0 {
		q.Where(agentrun.CreatedAtGTE(time.UnixMilli(req.FromMS)))
	}
	if req.ToMS > 0 {
		q.Where(agentrun.CreatedAtLTE(time.UnixMilli(req.ToMS)))
	}
	total, e := q.Count(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	rows, e := q.Order(agentrun.ByCreatedAt()).Offset((p - 1) * s).Limit(s).All(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	out := make([]*types.RunResp, 0, len(rows))
	for _, x := range rows {
		out = append(out, runDTO(x))
	}
	return &types.RunListResp{Data: out, Total: total, Page: p, PageSize: s}, nil
}
