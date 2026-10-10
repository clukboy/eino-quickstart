package model

import (
	"context"
	"eino-quickstart/ent/modelprovider"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type ListModelProvidersLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListModelProvidersLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListModelProvidersLogic {
	return &ListModelProvidersLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *ListModelProvidersLogic) ListModelProviders(req *types.ModelProviderListReq) (*types.ModelProviderListResp, error) {
	p, s := req.Page, req.PageSize
	if p < 1 {
		p = 1
	}
	if s <= 0 || s > 100 {
		s = 20
	}
	q := l.svcCtx.EntClient.ModelProvider.Query()
	total, e := q.Count(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	rows, e := q.Order(modelprovider.ByUpdatedAt()).Offset((p - 1) * s).Limit(s).All(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	out := make([]*types.ModelProviderResp, 0, len(rows))
	for _, x := range rows {
		out = append(out, providerDTO(x))
	}
	return &types.ModelProviderListResp{Data: out, Total: total, Page: p, PageSize: s}, nil
}
