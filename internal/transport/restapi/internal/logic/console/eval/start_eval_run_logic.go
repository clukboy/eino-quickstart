package eval

import (
	"context"
	"eino-quickstart/ent/evalcase"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"fmt"
	"github.com/zeromicro/go-zero/core/logx"
	"time"
)

type StartEvalRunLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewStartEvalRunLogic(ctx context.Context, svcCtx *svc.ServiceContext) *StartEvalRunLogic {
	return &StartEvalRunLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *StartEvalRunLogic) StartEvalRun(req *types.EvalRunStartReq) (*types.StartEvalRunResp, error) {
	n, e := l.svcCtx.EntClient.EvalCase.Query().Where(evalcase.EvalSetIDEQ(req.ID)).Count(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	runID := fmt.Sprintf("eval-%d", time.Now().UnixNano())
	x, e := l.svcCtx.EntClient.EvalRun.Create().SetRunID(runID).SetEvalSetID(req.ID).SetTotal(n).Save(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	return &types.StartEvalRunResp{RunID: x.RunID}, nil /* TODO: enqueue asynchronous evaluation and persist per-case results. */
}
