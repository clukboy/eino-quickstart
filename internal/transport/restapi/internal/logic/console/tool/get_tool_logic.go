package tool

import (
	"context"
	enttool "eino-quickstart/ent/tool"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type GetToolLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetToolLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetToolLogic {
	return &GetToolLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *GetToolLogic) GetTool(req *types.ToolKeyReq) (*types.ToolResp, error) {
	row, err := l.svcCtx.EntClient.Tool.Query().Where(enttool.KeyEQ(req.Key)).Only(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	return dto(row), nil
}
