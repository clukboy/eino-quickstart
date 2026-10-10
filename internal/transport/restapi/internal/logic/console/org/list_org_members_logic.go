package org

import (
	"context"
	"eino-quickstart/ent/orgmember"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type ListOrgMembersLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListOrgMembersLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListOrgMembersLogic {
	return &ListOrgMembersLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *ListOrgMembersLogic) ListOrgMembers(req *types.OrgUnitIDReq) (*types.OrgMemberListResp, error) {
	if _, e := l.svcCtx.EntClient.OrgUnit.Get(l.ctx, req.ID); e != nil {
		return nil, fail(e)
	}
	rows, e := l.svcCtx.EntClient.OrgMember.Query().Where(orgmember.OrgUnitIDEQ(req.ID)).Order(orgmember.ByJoinedAt()).All(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	out := make([]*types.OrgMemberResp, 0, len(rows))
	for _, x := range rows {
		out = append(out, memberDTO(x))
	}
	return &types.OrgMemberListResp{Data: out, Total: len(out)}, nil
}
