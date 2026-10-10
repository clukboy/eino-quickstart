package agent

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/ent/agentdef"
	"eino-quickstart/ent/agentversion"
	"eino-quickstart/internal/platform/storage/entx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type PublishAgentVersionLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewPublishAgentVersionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PublishAgentVersionLogic {
	return &PublishAgentVersionLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *PublishAgentVersionLogic) PublishAgentVersion(req *types.AgentVersionPathReq) (*types.AgentVersionResp, error) {
	var err error
	err = entx.WithTx(l.ctx, l.svcCtx.EntClient, func(tx *ent.Tx) error {
		if _, e := tx.AgentVersion.Update().Where(agentversion.AgentDefID(req.ID), agentversion.VersionEQ(req.Version), agentversion.StatusNEQ(agentversion.StatusRETIRED)).SetStatus(agentversion.StatusPUBLISHED).Save(l.ctx); e != nil {
			return e
		}
		if _, e := tx.AgentVersion.Update().Where(agentversion.AgentDefID(req.ID), agentversion.VersionNEQ(req.Version), agentversion.StatusEQ(agentversion.StatusPUBLISHED)).SetStatus(agentversion.StatusRETIRED).Save(l.ctx); e != nil {
			return e
		}
		return tx.AgentDef.UpdateOneID(req.ID).SetCurrentVersion(req.Version).SetPublishedVersion(req.Version).SetStatus(agentdef.StatusPUBLISHED).Exec(l.ctx)
	})
	if err != nil {
		return nil, fail(err)
	}
	row, e := l.svcCtx.EntClient.AgentVersion.Query().Where(agentversion.AgentDefID(req.ID), agentversion.VersionEQ(req.Version)).Only(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	return AgentVersionDTO(row), nil
}
