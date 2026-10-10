package model

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/ent/modelrouting"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type PutModelRoutingLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewPutModelRoutingLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PutModelRoutingLogic {
	return &PutModelRoutingLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *PutModelRoutingLogic) PutModelRouting(req *types.PutModelRoutingReq) (*types.ModelRoutingResp, error) {
	row, e := l.svcCtx.EntClient.ModelRouting.Query().Where(modelrouting.ConfigKeyEQ("default")).Only(l.ctx)
	if ent.IsNotFound(e) {
		row, e = l.svcCtx.EntClient.ModelRouting.Create().SetConfigKey("default").SetDefaultChatModel(req.DefaultChatModel).SetDefaultEmbeddingModel(req.DefaultEmbeddingModel).SetFallbackChain(req.FallbackChain).Save(l.ctx)
	} else if e == nil {
		row, e = l.svcCtx.EntClient.ModelRouting.UpdateOne(row).SetDefaultChatModel(req.DefaultChatModel).SetDefaultEmbeddingModel(req.DefaultEmbeddingModel).SetFallbackChain(req.FallbackChain).Save(l.ctx)
	}
	if e != nil {
		return nil, fail(e)
	}
	return routingDTO(row), nil
}
