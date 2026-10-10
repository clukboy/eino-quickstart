package user

import (
	"context"
	"errors"

	"eino-quickstart/internal/platform/persistence/account"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ResetUserPasswordLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewResetUserPasswordLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResetUserPasswordLogic {
	return &ResetUserPasswordLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *ResetUserPasswordLogic) ResetUserPassword(req *types.ResetUserPasswordReq) error {
	store, err := accountStore(l.svcCtx)
	if err != nil {
		return err
	}
	if err := account.ValidatePassword(req.Password); err != nil {
		return httpx.BadRequest(err.Error())
	}
	hash, err := account.HashPassword(req.Password, l.svcCtx.PasswordHashCost)
	if err != nil {
		return httpx.Internal("重置密码失败")
	}
	if err := store.ResetPassword(l.ctx, req.ID, hash); err != nil {
		if errors.Is(err, account.ErrNotFound) {
			return httpx.NotFound("用户不存在")
		}
		l.Errorf("reset user password failed id=%d: %v", req.ID, err)
		return httpx.Internal("重置密码失败")
	}
	return nil
}
