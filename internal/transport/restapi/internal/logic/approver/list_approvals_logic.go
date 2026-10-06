// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package approver

import (
	"context"

	"eino-quickstart/internal/platform/persistence/approval"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListApprovalsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListApprovalsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListApprovalsLogic {
	return &ListApprovalsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListApprovalsLogic) ListApprovals(req *types.ApprovalListReq) (resp *types.ApprovalListResp, err error) {
	page := req.Page
	if page <= 0 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	if page > 1_000_000 {
		return nil, httpx.BadRequest("page is too large")
	}

	result, err := l.svcCtx.Approvals.List(l.ctx, approval.ListQuery{
		Status: req.Status,
		Offset: (page - 1) * pageSize,
		Limit:  pageSize,
	})
	if err != nil {
		if req.Status != "" {
			return nil, httpx.BadRequest(err.Error())
		}
		return nil, httpx.Internal("list approvals failed")
	}

	items := make([]*types.ApprovalResp, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, approvalDTO(item))
	}

	return &types.ApprovalListResp{
		Data:     items,
		Total:    result.Total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}
