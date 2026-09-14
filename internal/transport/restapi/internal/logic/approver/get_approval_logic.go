// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package approver

import (
	"context"

	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetApprovalLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetApprovalLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetApprovalLogic {
	return &GetApprovalLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// GetApproval returns one approval record by id.
func (l *GetApprovalLogic) GetApproval(req *types.ApprovalPathReq) (resp *types.ApprovalResp, err error) {
	record, ok := l.svcCtx.Approvals.Get(l.ctx, req.ID)
	if !ok {
		return nil, httpx.NotFound("approval not found")
	}

	return approvalDTO(record), nil
}
