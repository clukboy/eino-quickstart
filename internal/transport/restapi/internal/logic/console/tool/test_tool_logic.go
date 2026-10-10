package tool

import (
	"context"
	enttool "eino-quickstart/ent/tool"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type TestToolLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTestToolLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TestToolLogic {
	return &TestToolLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *TestToolLogic) TestTool(req *types.ToolKeyReq) (*types.ToolTestResp, error) {
	if _, err := l.svcCtx.EntClient.Tool.Query().Where(enttool.KeyEQ(req.Key)).Only(l.ctx); err != nil {
		return nil, fail(err)
	}
	return &types.ToolTestResp{OK: false, Error: "TODO: tool execution is not implemented"}, nil
}
