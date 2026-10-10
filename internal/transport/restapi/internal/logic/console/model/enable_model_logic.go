package model

import (
	"context"
	"eino-quickstart/ent/model"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type EnableModelLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewEnableModelLogic(ctx context.Context, svcCtx *svc.ServiceContext) *EnableModelLogic {
	return &EnableModelLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *EnableModelLogic) EnableModel(req *types.EnableModelReq) (*types.ModelResp, error) {
	row, e := l.svcCtx.EntClient.Model.Query().Where(model.KeyEQ(req.Key)).Only(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	row, e = l.svcCtx.EntClient.Model.UpdateOne(row).SetEnabled(req.Enabled).Save(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	return modelDTO(row), nil
}
