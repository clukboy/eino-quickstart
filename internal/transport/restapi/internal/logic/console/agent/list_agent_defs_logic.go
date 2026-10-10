package agent

import (
	"context"
	"eino-quickstart/ent/agentdef"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
	"strings"
)

type ListAgentDefsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListAgentDefsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListAgentDefsLogic {
	return &ListAgentDefsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *ListAgentDefsLogic) ListAgentDefs(req *types.AgentDefListReq) (*types.AgentDefListResp, error) {
	page, size := req.Page, req.PageSize
	if page < 1 {
		page = 1
	}
	if size <= 0 || size > 100 {
		size = 20
	}
	q := l.svcCtx.EntClient.AgentDef.Query()
	if strings.TrimSpace(req.Keyword) != "" {
		q = q.Where(agentdef.Or(agentdef.NameContainsFold(req.Keyword), agentdef.DescriptionContainsFold(req.Keyword)))
	}
	if req.Status != "" {
		q = q.Where(agentdef.StatusEQ(agentdef.Status(req.Status)))
	}
	total, err := q.Count(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	rows, err := q.Order(agentdef.ByUpdatedAt()).Offset((page - 1) * size).Limit(size).All(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	out := make([]*types.AgentDefResp, 0, len(rows))
	for _, row := range rows {
		out = append(out, AgentDefDTO(row))
	}
	return &types.AgentDefListResp{Data: out, Total: total, Page: page, PageSize: size}, nil
}
