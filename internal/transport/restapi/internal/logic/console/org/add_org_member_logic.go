package org

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/ent/orgmember"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type AddOrgMemberLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAddOrgMemberLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AddOrgMemberLogic {
	return &AddOrgMemberLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *AddOrgMemberLogic) AddOrgMember(req *types.AddOrgMemberReq) (*types.OrgMemberResp, error) {
	if req.Subject == "" {
		return nil, httpx.BadRequest("subject is required")
	}
	u, e := l.svcCtx.EntClient.OrgUnit.Get(l.ctx, req.ID)
	if e != nil {
		return nil, fail(e)
	}
	x, e := l.svcCtx.EntClient.OrgMember.Create().SetOrgUnit(u).SetSubject(req.Subject).Save(l.ctx)
	if e != nil {
		if ent.IsConstraintError(e) {
			return nil, httpx.Conflict("subject is already a member")
		}
		return nil, fail(e)
	}
	_ = orgmember.FieldSubject
	return memberDTO(x), nil
}
