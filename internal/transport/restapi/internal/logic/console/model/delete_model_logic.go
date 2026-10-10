package model

import (
	"context"
	"eino-quickstart/ent/model"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type DeleteModelLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDeleteModelLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteModelLogic {
	return &DeleteModelLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *DeleteModelLogic) DeleteModel(req *types.ModelProviderKeyReq) (*types.StatusResp, error) {
	row, e := l.svcCtx.EntClient.Model.Query().Where(model.KeyEQ(req.Key)).Only(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	if e = l.svcCtx.EntClient.Model.DeleteOne(row).Exec(l.ctx); e != nil {
		return nil, fail(e)
	}
	return &types.StatusResp{Status: "deleted"}, nil
}
