// Code scaffolded by goctl. Safe to edit.

package user

import (
	"context"

	"eino-quickstart/internal/application/usersync"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type SyncUsersLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSyncUsersLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SyncUsersLogic {
	return &SyncUsersLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *SyncUsersLogic) SyncUsers(req *types.SyncUsersReq) (*types.SyncUsersResp, error) {
	if l.svcCtx.UserSync == nil {
		return nil, httpx.Unavailable("用户同步功能未开启")
	}
	report, err := l.svcCtx.UserSync.Sync(l.ctx, req.DryRun)
	if err != nil {
		l.Errorf("user sync failed: %v", err)
		return nil, httpx.New(502, "bad_gateway", "外部用户源同步失败")
	}
	return syncResponse(report), nil
}

func syncResponse(report *usersync.Report) *types.SyncUsersResp {
	resp := &types.SyncUsersResp{
		DryRun:  report.DryRun,
		Total:   report.Total,
		Created: report.Created,
		Updated: report.Updated,
		Skipped: report.Skipped,
		Failed:  report.Failed,
	}
	for _, created := range report.CreatedUsers {
		resp.CreatedUsers = append(resp.CreatedUsers, types.SyncCreatedUser{
			ExternalID:      created.ExternalID,
			Username:        created.Username,
			InitialPassword: created.InitialPassword,
		})
	}
	for _, item := range report.Errors {
		resp.Errors = append(resp.Errors, types.SyncUserError{
			ExternalID: item.ExternalID,
			Username:   item.Username,
			Reason:     item.Reason,
		})
	}
	return resp
}
