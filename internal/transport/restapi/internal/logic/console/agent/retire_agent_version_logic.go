package agent

import (
	"context"
	"eino-quickstart/ent/agentversion"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type RetireAgentVersionLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRetireAgentVersionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RetireAgentVersionLogic {
	return &RetireAgentVersionLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *RetireAgentVersionLogic) RetireAgentVersion(req *types.AgentVersionPathReq) (*types.AgentVersionResp, error) {
	row, err := l.svcCtx.EntClient.AgentVersion.Query().Where(agentversion.AgentDefID(req.ID), agentversion.VersionEQ(req.Version)).Only(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	row, err = l.svcCtx.EntClient.AgentVersion.UpdateOne(row).SetStatus(agentversion.StatusRETIRED).Save(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	return AgentVersionDTO(row), nil
}
