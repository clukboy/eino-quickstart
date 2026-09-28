// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package agent

import (
	"context"

	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListSessionMessagesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 列出会话的历史消息
func NewListSessionMessagesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListSessionMessagesLogic {
	return &ListSessionMessagesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// ListSessionMessages 按时间正序返回一个会话的历史消息（user 与 assistant 混排）。
//
// 「刚建好、还没说话」的会话返回空数组而不是 404 —— 归属校验在 Store 里单独
// 做过了，所以这里的空只可能是「真的一条消息都没有」。这两件事必须分得开：
// 把越权也返回空列表，就等于让攻击者用「有没有报错」探测 uuid 是否存在。
func (l *ListSessionMessagesLogic) ListSessionMessages(req *types.SessionMessagesReq) (resp *types.SessionMessagesResp, err error) {
	subject, err := actorSubject(l.ctx)
	if err != nil {
		return nil, err
	}

	// req.ID 是会话的对外 id（uuid），不是自增主键 —— 见 agent.api 的说明。
	records, err := l.svcCtx.Sessions.Messages(l.ctx, req.ID, subject)
	if err != nil {
		return nil, fail(err)
	}

	result := make([]*types.SessionMessageResp, 0, len(records))
	for _, record := range records {
		result = append(result, messageDTO(record))
	}

	return &types.SessionMessagesResp{Data: result}, nil
}
