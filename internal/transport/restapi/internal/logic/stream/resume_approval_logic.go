// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package stream

import (
	"context"

	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ResumeApprovalLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewResumeApprovalLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResumeApprovalLogic {
	return &ResumeApprovalLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ResumeApprovalLogic) ResumeApproval(req *types.ResumeApprovalPathReq, client chan<- *types.ChatEvent) error {
	// todo: add your logic here and delete this line

	return nil
}
