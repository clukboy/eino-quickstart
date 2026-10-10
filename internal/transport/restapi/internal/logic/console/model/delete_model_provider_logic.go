package model

import (
	"context"
	"eino-quickstart/ent/model"
	"eino-quickstart/ent/modelprovider"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type DeleteModelProviderLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDeleteModelProviderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteModelProviderLogic {
	return &DeleteModelProviderLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *DeleteModelProviderLogic) DeleteModelProvider(req *types.ModelProviderKeyReq) (*types.StatusResp, error) {
	p, e := l.svcCtx.EntClient.ModelProvider.Query().Where(modelprovider.KeyEQ(req.Key)).Only(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	n, e := l.svcCtx.EntClient.Model.Query().Where(model.ProviderID(p.ID)).Count(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	if n > 0 {
		return nil, httpx.Conflict("provider is still used by models")
	}
	if e = l.svcCtx.EntClient.ModelProvider.DeleteOne(p).Exec(l.ctx); e != nil {
		return nil, fail(e)
	}
	return &types.StatusResp{Status: "deleted"}, nil
}
