package agent

import (
	"context"
	"eino-quickstart/ent/agentdef"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
	"strings"
)

type UpdateAgentDefLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateAgentDefLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateAgentDefLogic {
	return &UpdateAgentDefLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UpdateAgentDefLogic) UpdateAgentDef(req *types.UpdateAgentDefReq) (*types.AgentDefResp, error) {
	q := l.svcCtx.EntClient.AgentDef.UpdateOneID(req.ID)
	if strings.TrimSpace(req.Name) != "" {
		q.SetName(strings.TrimSpace(req.Name))
	}
	if req.Description != "" {
		q.SetDescription(req.Description)
	}
	if req.Model != "" {
		q.SetModel(req.Model)
	}
	if req.RiskTier != "" {
		q.SetRiskTier(req.RiskTier)
	}
	row, err := q.Save(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	_ = agentdef.FieldID
	return AgentDefDTO(row), nil
}
