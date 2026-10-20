package run

import (
	"context"

	"eino-quickstart/ent/chatturn"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetRunLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetRunLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetRunLogic {
	return &GetRunLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

// GetRun 返回一轮对话的详情。
//
// 三个附加数组的现状：spans 与 hits 没有数据源（run_spans / run_retrieval_hits
// 从来没有写入路径，表已删除），所以恒为空；messages 由这一轮自己的提问和回答
// 拼出来 —— 这正是排查「答得不对」时要看的两句话。
func (l *GetRunLogic) GetRun(req *types.RunPathReq) (*types.RunDetailResp, error) {
	turn, err := l.svcCtx.EntClient.ChatTurn.
		Query().
		Where(chatturn.TurnIDEQ(req.RunID)).
		Only(l.ctx)
	if err != nil {
		return nil, fail(err)
	}

	messages := []*types.RunMessageResp{
		{
			Role:      "user",
			Content:   turn.UserContent,
			CreatedAt: turn.CreatedAt.UnixMilli(),
		},
	}
	if turn.AssistantContent != nil && *turn.AssistantContent != "" {
		createdAt := turn.CreatedAt.UnixMilli()
		if turn.CompletedAt != nil {
			createdAt = turn.CompletedAt.UnixMilli()
		}
		messages = append(messages, &types.RunMessageResp{
			Role:      "assistant",
			Content:   *turn.AssistantContent,
			CreatedAt: createdAt,
		})
	}

	return &types.RunDetailResp{
		Run:      turnDTO(turn),
		Spans:    make([]*types.RunSpanResp, 0),
		Hits:     make([]*types.SearchHitResp, 0),
		Messages: messages,
	}, nil
}
