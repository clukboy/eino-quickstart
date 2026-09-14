// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package approver

import (
	"context"

	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type DecideApprovalLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDecideApprovalLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DecideApprovalLogic {
	return &DecideApprovalLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// DecideApproval approves or rejects a pending approval.
func (l *DecideApprovalLogic) DecideApproval(req *types.ApprovalDecisionReq) (resp *types.StatusResp, err error) {
	identity, ok := auth.IdentityFromContext(l.ctx)
	if !ok {
		return nil, httpx.Unauthorized("unauthorized")
	}

	if err = l.svcCtx.Approvals.Decide(l.ctx, req.ID, req.Approved, identity.Subject); err != nil {
		return nil, httpx.BadRequest(err.Error())
	}

	return &types.StatusResp{Status: "ok"}, nil
}
