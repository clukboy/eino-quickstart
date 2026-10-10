package agent

import (
	"context"
	"strings"

	"eino-quickstart/ent/agentdef"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type CreateAgentDefLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateAgentDefLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateAgentDefLogic {
	return &CreateAgentDefLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *CreateAgentDefLogic) CreateAgentDef(req *types.CreateAgentDefReq) (*types.AgentDefResp, error) {
	identity, ok := auth.IdentityFromContext(l.ctx)
	if !ok {
		return nil, httpx.Unauthorized("unauthorized")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, httpx.BadRequest("agent name is required")
	}
	risk := strings.TrimSpace(req.RiskTier)
	if risk == "" {
		risk = "T1"
	}
	row, err := l.svcCtx.EntClient.AgentDef.Create().SetName(name).SetDescription(req.Description).
		SetOwnerSubject(identity.Subject).SetModel(req.Model).SetRiskTier(risk).SetStatus(agentdef.StatusDRAFT).Save(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	return AgentDefDTO(row), nil
}
