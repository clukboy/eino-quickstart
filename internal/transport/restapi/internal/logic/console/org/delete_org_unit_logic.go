package org

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/ent/orgmember"
	"eino-quickstart/ent/orgunit"
	"eino-quickstart/internal/platform/storage/entx"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type DeleteOrgUnitLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDeleteOrgUnitLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteOrgUnitLogic {
	return &DeleteOrgUnitLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *DeleteOrgUnitLogic) DeleteOrgUnit(req *types.OrgUnitIDReq) (*types.StatusResp, error) {
	e := entx.WithTx(l.ctx, l.svcCtx.EntClient, func(tx *ent.Tx) error {
		if _, e := tx.OrgUnit.Get(l.ctx, req.ID); e != nil {
			return e
		}
		if n, e := tx.OrgUnit.Query().Where(orgunit.ParentIDEQ(req.ID)).Count(l.ctx); e != nil {
			return e
		} else if n > 0 {
			return httpx.Conflict("organization unit has children")
		}
		if _, e := tx.OrgMember.Delete().Where(orgmember.OrgUnitIDEQ(req.ID)).Exec(l.ctx); e != nil {
			return e
		}
		return tx.OrgUnit.DeleteOneID(req.ID).Exec(l.ctx)
	})
	if e != nil {
		if _, ok := e.(*httpx.Error); ok {
			return nil, e
		}
		return nil, fail(e)
	}
	return &types.StatusResp{Status: "deleted"}, nil
}
