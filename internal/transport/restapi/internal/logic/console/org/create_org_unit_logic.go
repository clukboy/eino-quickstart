package org

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/internal/platform/storage/entx"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"fmt"
	"github.com/zeromicro/go-zero/core/logx"
)

type CreateOrgUnitLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateOrgUnitLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateOrgUnitLogic {
	return &CreateOrgUnitLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *CreateOrgUnitLogic) CreateOrgUnit(req *types.CreateOrgUnitReq) (*types.OrgUnitResp, error) {
	if req.Name == "" {
		return nil, httpx.BadRequest("organization name is required")
	}
	var out *ent.OrgUnit
	err := entx.WithTx(l.ctx, l.svcCtx.EntClient, func(tx *ent.Tx) error {
		b := tx.OrgUnit.Create().SetName(req.Name)
		if req.ParentID > 0 {
			p, e := tx.OrgUnit.Get(l.ctx, req.ParentID)
			if e != nil {
				return e
			}
			b.SetParent(p)
		}
		x, e := b.Save(l.ctx)
		if e != nil {
			return e
		}
		path := fmt.Sprintf("/%d", x.ID)
		if req.ParentID > 0 {
			p, e := tx.OrgUnit.Get(l.ctx, req.ParentID)
			if e != nil {
				return e
			}
			if p.Path != nil {
				path = *p.Path + path
			}
		}
		x, e = tx.OrgUnit.UpdateOne(x).SetPath(path).Save(l.ctx)
		out = x
		return e
	})
	if err != nil {
		if _, ok := err.(*httpx.Error); ok {
			return nil, err
		}
		return nil, fail(err)
	}
	return unitDTO(out, 0), nil
}
