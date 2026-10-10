package org

import (
	"context"
	"eino-quickstart/ent/orgunit"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type ListOrgUnitsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListOrgUnitsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListOrgUnitsLogic {
	return &ListOrgUnitsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *ListOrgUnitsLogic) ListOrgUnits() (*types.OrgUnitListResp, error) {
	rows, e := l.svcCtx.EntClient.OrgUnit.Query().Order(orgunit.ByPath()).All(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	out := make([]*types.OrgUnitResp, 0, len(rows))
	for _, x := range rows {
		n, e := x.QueryMembers().Count(l.ctx)
		if e != nil {
			return nil, fail(e)
		}
		out = append(out, unitDTO(x, n))
	}
	return &types.OrgUnitListResp{Data: out}, nil
}
