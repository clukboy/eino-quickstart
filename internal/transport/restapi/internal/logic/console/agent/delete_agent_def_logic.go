package agent

import (
	"context"
	"eino-quickstart/ent/agentdef"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type DeleteAgentDefLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDeleteAgentDefLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteAgentDefLogic {
	return &DeleteAgentDefLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *DeleteAgentDefLogic) DeleteAgentDef(req *types.AgentDefIDReq) (*types.StatusResp, error) {
	n, err := l.svcCtx.EntClient.AgentDef.UpdateOneID(req.ID).SetStatus(agentdef.StatusARCHIVED).Save(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	_ = n
	return &types.StatusResp{Status: "archived"}, nil
}
