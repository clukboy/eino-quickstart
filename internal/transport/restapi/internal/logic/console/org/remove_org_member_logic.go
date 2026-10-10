package org

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/ent/orgmember"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type RemoveOrgMemberLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRemoveOrgMemberLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RemoveOrgMemberLogic {
	return &RemoveOrgMemberLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *RemoveOrgMemberLogic) RemoveOrgMember(req *types.RemoveOrgMemberReq) (*types.StatusResp, error) {
	n, e := l.svcCtx.EntClient.OrgMember.Delete().Where(orgmember.OrgUnitIDEQ(req.ID), orgmember.SubjectEQ(req.Subject)).Exec(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	if n == 0 {
		return nil, fail(&ent.NotFoundError{})
	}
	return &types.StatusResp{Status: "deleted"}, nil
}
