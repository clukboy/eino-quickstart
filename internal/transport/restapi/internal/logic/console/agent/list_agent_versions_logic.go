package agent

import (
	"context"
	"eino-quickstart/ent/agentversion"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type ListAgentVersionsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListAgentVersionsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListAgentVersionsLogic {
	return &ListAgentVersionsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *ListAgentVersionsLogic) ListAgentVersions(req *types.AgentDefIDReq) (*types.AgentVersionListResp, error) {
	rows, err := l.svcCtx.EntClient.AgentVersion.Query().Where(agentversion.AgentDefID(req.ID)).Order(agentversion.ByVersion()).All(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	out := make([]*types.AgentVersionResp, 0, len(rows))
	for _, row := range rows {
		out = append(out, AgentVersionDTO(row))
	}
	return &types.AgentVersionListResp{Data: out}, nil
}
