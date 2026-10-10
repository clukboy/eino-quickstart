package model

import (
	"context"
	"eino-quickstart/ent/modelrouting"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type GetModelRoutingLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetModelRoutingLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetModelRoutingLogic {
	return &GetModelRoutingLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *GetModelRoutingLogic) GetModelRouting() (*types.ModelRoutingResp, error) {
	row, e := l.svcCtx.EntClient.ModelRouting.Query().Where(modelrouting.ConfigKeyEQ("default")).Only(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	return routingDTO(row), nil
}
