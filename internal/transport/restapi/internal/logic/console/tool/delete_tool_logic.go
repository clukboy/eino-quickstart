package tool

import (
	"context"
	enttool "eino-quickstart/ent/tool"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type DeleteToolLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDeleteToolLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteToolLogic {
	return &DeleteToolLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *DeleteToolLogic) DeleteTool(req *types.ToolKeyReq) (*types.StatusResp, error) {
	row, err := l.svcCtx.EntClient.Tool.Query().Where(enttool.KeyEQ(req.Key)).Only(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	if row.Kind == enttool.KindBuiltin {
		return nil, httpx.Conflict("builtin tools cannot be deleted")
	}
	if err := l.svcCtx.EntClient.Tool.DeleteOne(row).Exec(l.ctx); err != nil {
		return nil, fail(err)
	}
	return &types.StatusResp{Status: "deleted"}, nil
}
