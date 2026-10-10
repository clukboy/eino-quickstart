package eval

import (
	"context"
	"eino-quickstart/ent/evalrun"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"entgo.io/ent/dialect/sql"
	"github.com/zeromicro/go-zero/core/logx"
)

type ListEvalRunsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListEvalRunsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListEvalRunsLogic {
	return &ListEvalRunsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *ListEvalRunsLogic) ListEvalRuns(req *types.EvalSetIDReq) (*types.EvalRunListResp, error) {
	rows, e := l.svcCtx.EntClient.EvalRun.Query().Where(evalrun.EvalSetIDEQ(req.ID)).Order(evalrun.ByStartedAt(sql.OrderDesc())).All(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	out := make([]*types.EvalRunResp, 0, len(rows))
	for _, x := range rows {
		out = append(out, runDTO(x))
	}
	return &types.EvalRunListResp{Data: out, Total: len(out)}, nil
}
