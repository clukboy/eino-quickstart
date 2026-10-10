package approval

import (
	"context"
	"eino-quickstart/ent/approvalpolicy"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type ListApprovalPoliciesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListApprovalPoliciesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListApprovalPoliciesLogic {
	return &ListApprovalPoliciesLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *ListApprovalPoliciesLogic) ListApprovalPolicies() (*types.ApprovalPolicyListResp, error) {
	rows, e := l.svcCtx.EntClient.ApprovalPolicy.Query().Order(approvalpolicy.ByToolKey(), approvalpolicy.ByRiskTier()).All(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	out := make([]*types.ApprovalPolicyResp, 0, len(rows))
	for _, x := range rows {
		out = append(out, dto(x))
	}
	return &types.ApprovalPolicyListResp{Data: out}, nil
}
