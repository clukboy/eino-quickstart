// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package agent

import (
	"context"

	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListSessionsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 列出我的会话（按最后活动时间倒序）
func NewListSessionsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListSessionsLogic {
	return &ListSessionsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// ListSessions 返回当前调用方的全部会话，最新的在前。
//
// 不带分页、不过滤空会话，两件事都在 agent.api 里写了理由。这里只强调一点：
// owner 取自 token，接口上没有、也不该有「查谁的会话」这个参数。
func (l *ListSessionsLogic) ListSessions() (resp *types.SessionListResp, err error) {
	subject, err := actorSubject(l.ctx)
	if err != nil {
		return nil, err
	}

	records, err := l.svcCtx.Sessions.List(l.ctx, subject)
	if err != nil {
		return nil, fail(err)
	}

	// 空列表要返回 []，不能返回 null —— 前者是 JSON 数组、后者是 JSON 对象，
	// 客户端按数组解会在「一条会话都没有」这个最常见的首次使用场景里炸掉。
	result := make([]*types.SessionResp, 0, len(records))
	for _, record := range records {
		result = append(result, sessionDTO(record))
	}

	return &types.SessionListResp{Data: result}, nil
}
