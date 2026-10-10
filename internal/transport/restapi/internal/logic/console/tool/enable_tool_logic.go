package tool

import (
	"context"
	enttool "eino-quickstart/ent/tool"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type EnableToolLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewEnableToolLogic(ctx context.Context, svcCtx *svc.ServiceContext) *EnableToolLogic {
	return &EnableToolLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *EnableToolLogic) EnableTool(req *types.EnableToolReq) (*types.ToolResp, error) {
	row, err := l.svcCtx.EntClient.Tool.Query().Where(enttool.KeyEQ(req.Key)).Only(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	row, err = l.svcCtx.EntClient.Tool.UpdateOne(row).SetEnabled(req.Enabled).Save(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	return dto(row), nil
}
