package user

import (
	"context"
	"errors"

	entUser "eino-quickstart/ent/user"
	"eino-quickstart/internal/platform/persistence/account"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateUserLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserLogic {
	return &UpdateUserLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *UpdateUserLogic) UpdateUser(req *types.UpdateUserReq) (*types.UserResp, error) {
	store, err := accountStore(l.svcCtx)
	if err != nil {
		return nil, err
	}
	if req.Nickname == "" && req.Role == "" && req.Status == "" {
		return nil, httpx.BadRequest("至少提供一个需要修改的字段")
	}
	var nickname string
	if req.Nickname != "" {
		nickname, err = normalizeNickname(req.Nickname)
		if err != nil {
			return nil, err
		}
	}
	if req.Role != "" {
		if req.Role != string(entUser.RoleAgent) && req.Role != string(entUser.RoleAdmin) {
			return nil, httpx.BadRequest("角色必须是 agent 或 admin")
		}
	}
	if req.Status != "" && req.Status != string(entUser.StatusACTIVE) && req.Status != string(entUser.StatusDISABLED) {
		return nil, httpx.BadRequest("状态必须是 ACTIVE 或 DISABLED")
	}
	record, err := store.Update(l.ctx, req.ID, nickname, req.Role, req.Status)
	if errors.Is(err, account.ErrNotFound) {
		return nil, httpx.NotFound("用户不存在")
	}
	if err != nil {
		l.Errorf("update user failed id=%d: %v", req.ID, err)
		return nil, httpx.Internal("修改用户失败")
	}
	resp := userDTO(record)
	return &resp, nil
}
