package org

import (
	"context"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateOrgUnitLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateOrgUnitLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateOrgUnitLogic {
	return &UpdateOrgUnitLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UpdateOrgUnitLogic) UpdateOrgUnit(req *types.UpdateOrgUnitReq) (*types.OrgUnitResp, error) {
	x, e := l.svcCtx.EntClient.OrgUnit.Get(l.ctx, req.ID)
	if e != nil {
		return nil, fail(e)
	}
	u := l.svcCtx.EntClient.OrgUnit.UpdateOne(x)
	if req.Name != "" {
		u.SetName(req.Name)
	}
	if req.ParentID > 0 && req.ParentID != x.ID {
		p, e := l.svcCtx.EntClient.OrgUnit.Get(l.ctx, req.ParentID)
		if e != nil {
			return nil, fail(e)
		}
		u.SetParent(p)
	} else if req.ParentID == 0 {
		u.ClearParent()
	}
	x, e = u.Save(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	n, e := x.QueryMembers().Count(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	return unitDTO(x, n), nil /* TODO: recalculate descendant paths when moving a subtree. */
}
