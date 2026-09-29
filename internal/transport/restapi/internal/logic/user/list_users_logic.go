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

// ListUsers 返回全部账号，新建的排在最前。
//
// 不分页（与 /sessions 同一取舍）：账号在「人工建号、不允许注册」的前提下天然
// 很少，而分页会让前端的用户管理页多出一整套翻页状态。真到了要分页的时候，
// 加的应该是显式的 page/pageSize 参数，而不是在 store 里塞一个不透明的默认
// 上限 —— 后者会让「第 51 个人去哪了」变成一个查不出来的问题。
//
// 这里**没有**任何所有权过滤：本组挂 RoleAdmin，能进来的只有管理员，而「系统里
// 有哪些人」正是他需要知道的。
func (l *ListUsersLogic) ListUsers() (resp *types.UserListResp, err error) {
	store, err := accountStore(l.svcCtx)
	if err != nil {
		return nil, err
	}

	records, err := store.List(l.ctx)
	if err != nil {
		l.Errorf("list accounts failed: %v", err)
		return nil, httpx.Internal("查询账号列表失败")
	}

	// 用 make(..., 0, n) 而不是 var data []types.UserResp：后者在空列表时会被
	// 序列化成 null，前端要为此多写一个 `?? []`。空数组才是这个接口的契约。
	data := make([]types.UserResp, 0, len(records))
	for _, record := range records {
		data = append(data, userDTO(record))
	}

	return &types.UserListResp{Data: data, Total: len(data)}, nil
}
