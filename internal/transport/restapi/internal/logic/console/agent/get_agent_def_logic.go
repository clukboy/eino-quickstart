package agent

import (
	"context"
	"eino-quickstart/ent/agentdef"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type GetAgentDefLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetAgentDefLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetAgentDefLogic {
	return &GetAgentDefLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *GetAgentDefLogic) GetAgentDef(req *types.AgentDefIDReq) (*types.AgentDefResp, error) {
	row, err := l.svcCtx.EntClient.AgentDef.Get(l.ctx, req.ID)
	if err != nil {
		return nil, fail(err)
	}
	_ = agentdef.FieldID
	return AgentDefDTO(row), nil
}
