package approval

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/internal/platform/storage/entx"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type PutApprovalPoliciesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewPutApprovalPoliciesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PutApprovalPoliciesLogic {
	return &PutApprovalPoliciesLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *PutApprovalPoliciesLogic) PutApprovalPolicies(req *types.PutApprovalPoliciesReq) (*types.ApprovalPolicyListResp, error) {
	e := entx.WithTx(l.ctx, l.svcCtx.EntClient, func(tx *ent.Tx) error {
		if _, err := tx.ApprovalPolicy.Delete().Exec(l.ctx); err != nil {
			return err
		}
		for _, p := range req.Data {
			if p == nil || p.ToolKey == "" || p.RiskTier == "" {
				return httpx.BadRequest("approval policy tool_key and risk_tier are required")
			}
			m, err := parseMode(p.Mode)
			if err != nil {
				return httpx.BadRequest("invalid approval policy mode")
			}
			if _, err = tx.ApprovalPolicy.Create().SetToolKey(p.ToolKey).SetRiskTier(p.RiskTier).SetMode(m).SetApproverRoles(p.ApproverRoles).SetQuorum(p.Quorum).SetTimeoutSeconds(p.TimeoutSeconds).SetOnTimeout(p.OnTimeout).SetEnabled(p.Enabled).Save(l.ctx); err != nil {
				return err
			}
		}
		return nil
	})
	if e != nil {
		if _, ok := e.(*httpx.Error); ok {
			return nil, e
		}
		return nil, fail(e)
	}
	return (&ListApprovalPoliciesLogic{ctx: l.ctx, svcCtx: l.svcCtx}).ListApprovalPolicies()
}
