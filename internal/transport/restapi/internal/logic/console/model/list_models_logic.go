package model

import (
	"context"
	"eino-quickstart/ent/model"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type ListModelsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListModelsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListModelsLogic {
	return &ListModelsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *ListModelsLogic) ListModels(req *types.ModelListReq) (*types.ModelListResp, error) {
	p, s := req.Page, req.PageSize
	if p < 1 {
		p = 1
	}
	if s <= 0 || s > 100 {
		s = 20
	}
	q := l.svcCtx.EntClient.Model.Query()
	if req.Kind != "" {
		q = q.Where(model.KindEQ(model.Kind(req.Kind)))
	}
	if req.ProviderKey != "" {
		q = q.Where(model.ProviderKeyEQ(req.ProviderKey))
	}
	total, e := q.Count(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	rows, e := q.Order(model.ByUpdatedAt()).Offset((p - 1) * s).Limit(s).All(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	out := make([]*types.ModelResp, 0, len(rows))
	for _, x := range rows {
		out = append(out, modelDTO(x))
	}
	return &types.ModelListResp{Data: out, Total: total, Page: p, PageSize: s}, nil
}
