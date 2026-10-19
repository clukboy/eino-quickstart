package stream

import (
	"context"
	"time"

	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type TaskStreamLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTaskStreamLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TaskStreamLogic {
	return &TaskStreamLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *TaskStreamLogic) TaskStream(client chan<- *types.TaskEvent) error {
	for {
		select {
		case <-l.ctx.Done():
			return l.ctx.Err()
		case <-time.After(2 * time.Hour):
			client <- &types.TaskEvent{
				ID:    "1",
				Event: "task.new",
				Data: struct {
					Msg string `json:"msg"`
				}{
					Msg: "测试一下",
				},
			}
		}
	}
	return nil
}
