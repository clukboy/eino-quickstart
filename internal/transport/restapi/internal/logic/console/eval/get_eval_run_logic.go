package eval

import (
	"context"
	"eino-quickstart/ent/evalcaseresult"
	"eino-quickstart/ent/evalrun"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type GetEvalRunLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetEvalRunLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetEvalRunLogic {
	return &GetEvalRunLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *GetEvalRunLogic) GetEvalRun(req *types.EvalRunIDReq) (*types.EvalRunDetailResp, error) {
	x, e := l.svcCtx.EntClient.EvalRun.Query().Where(evalrun.RunIDEQ(req.RunID)).Only(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	rows, e := l.svcCtx.EntClient.EvalCaseResult.Query().Where(evalcaseresult.EvalRunIDEQ(x.ID)).Order(evalcaseresult.ByCaseID()).All(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	out := make([]*types.EvalCaseResultResp, 0, len(rows))
	for _, r := range rows {
		out = append(out, resultDTO(r))
	}
	return &types.EvalRunDetailResp{Run: runDTO(x), Results: out}, nil
}
