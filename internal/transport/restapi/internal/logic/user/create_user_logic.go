// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

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

type CreateUserLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 创建账号（角色固定 agent，初始密码由管理员设定并强制首登改密）
func NewCreateUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateUserLogic {
	return &CreateUserLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateUserLogic) CreateUser(req *types.CreateUserReq) (resp *types.UserResp, err error) {
	store, err := accountStore(l.svcCtx)
	if err != nil {
		return nil, err
	}

	username, err := normalizeUsername(req.Username)
	if err != nil {
		return nil, err
	}

	nickname, err := normalizeNickname(req.Nickname)
	if err != nil {
		return nil, err
	}

	if err := account.ValidatePassword(req.Password); err != nil {
		return nil, httpx.BadRequest(err.Error())
	}

	passwordHash, err := account.HashPassword(req.Password, l.svcCtx.PasswordHashCost)
	if err != nil {
		l.Errorf("hash initial password failed username=%s: %v", username, err)
		return nil, httpx.Internal("创建账号失败")
	}

	record, err := store.Create(l.ctx, account.CreateParams{
		Username: username,
		Nickname: nickname,
		PasswordHash: passwordHash,
	})
	if errors.Is(err, account.ErrUsernameTaken) {
		return nil, httpx.Conflict("用户名已存在")
	}
	if err != nil {
		l.Errorf("create account failed username=%s: %v", username, err)
		return nil, httpx.Internal("创建账号失败")
	}
	
	dto := userDTO(record)

	return &dto, nil
}
