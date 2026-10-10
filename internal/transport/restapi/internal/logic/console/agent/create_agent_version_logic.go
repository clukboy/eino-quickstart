package agent

import (
	"context"
	"strings"

	"eino-quickstart/ent/agentversion"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type CreateAgentVersionLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateAgentVersionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateAgentVersionLogic {
	return &CreateAgentVersionLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *CreateAgentVersionLogic) CreateAgentVersion(req *types.CreateAgentVersionReq) (*types.AgentVersionResp, error) {
	identity, ok := auth.IdentityFromContext(l.ctx)
	if !ok {
		return nil, httpx.Unauthorized("unauthorized")
	}
	base, err := l.svcCtx.EntClient.AgentDef.Get(l.ctx, req.ID)
	if err != nil {
		return nil, fail(err)
	}
	version := 1
	last, err := l.svcCtx.EntClient.AgentVersion.Query().Where(agentversion.AgentDefID(req.ID)).Order(agentversion.ByVersion()).First(l.ctx)
	if err == nil {
		version = last.Version + 1
	}
	if req.SystemPrompt == "" {
		req.SystemPrompt = ""
	}
	row, err := l.svcCtx.EntClient.AgentVersion.Create().SetAgentDefID(req.ID).SetVersion(version).
		SetSystemPrompt(req.SystemPrompt).SetModel(base.Model).SetToolKeys(req.ToolKeys).SetDatasetIds(req.DatasetIDs).
		SetChangeNote(strings.TrimSpace(req.ChangeNote)).SetCreatedBy(identity.Subject).Save(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	return AgentVersionDTO(row), nil
}
