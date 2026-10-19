package org

import (
	"context"
	"eino-quickstart/ent/orgmember"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
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
func (l *ListOrgMembersLogic) ListOrgMembers(req *types.OrgMemberListReq) (*types.OrgMemberListResp, error) {
	// 先确认部门存在：不存在回 404 而不是空列表，否则「部门选错了」与
	// 「这个部门还没加人」在界面上长得一模一样。
	if _, e := l.svcCtx.EntClient.OrgUnit.Get(l.ctx, req.ID); e != nil {
		return nil, fail(e)
	}

	page, pageSize := httpx.NormalizePage(req.Page, req.PageSize)

	q := l.svcCtx.EntClient.OrgMember.Query().Where(orgmember.OrgUnitIDEQ(req.ID))

	// Count 在加 Order / Offset 之前问：总数与排序、分页都无关。
	total, e := q.Count(l.ctx)
	if e != nil {
		return nil, fail(e)
	}

	rows, e := q.
		Order(orgmember.ByJoinedAt()).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(l.ctx)
	if e != nil {
		return nil, fail(e)
	}

	out := make([]*types.OrgMemberResp, 0, len(rows))
	for _, x := range rows {
		out = append(out, memberDTO(x))
	}

	// Total 是该部门的成员总数，不是本页条数：前端据它算总页数。
	return &types.OrgMemberListResp{Data: out, Total: total, Page: page, PageSize: pageSize}, nil
}
