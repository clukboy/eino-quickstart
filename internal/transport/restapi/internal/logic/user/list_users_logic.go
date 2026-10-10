// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package user

import (
	"context"

	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListUsersLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 列出全部账号（按创建时间倒序，暂不分页）
func NewListUsersLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListUsersLogic {
	return &ListUsersLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListUsersLogic) ListUsers(req *types.UserListReq) (resp *types.UserListResp, err error) {
	store, err := accountStore(l.svcCtx)
	if err != nil {
		return nil, err
	}
	
	records, err := store.List(l.ctx, normalizeKeyword(req.Keyword))
	if err != nil {
		l.Errorf("list accounts failed: %v", err)
		return nil, httpx.Internal("查询账号列表失败")
	}

	data := make([]types.UserResp, 0, len(records))
	for _, record := range records {
		data = append(data, userDTO(record))
	}

	return &types.UserListResp{Data: data, Total: len(data)}, nil
}
