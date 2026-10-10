package eval

import (
	"context"
	"eino-quickstart/ent/evalcase"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type DeleteEvalCaseLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDeleteEvalCaseLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteEvalCaseLogic {
	return &DeleteEvalCaseLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *DeleteEvalCaseLogic) DeleteEvalCase(req *types.EvalCasePathReq) (*types.StatusResp, error) {
	x, e := l.svcCtx.EntClient.EvalCase.Query().Where(evalcase.IDEQ(req.CaseID), evalcase.EvalSetIDEQ(req.ID)).Only(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	if n, e := x.QueryResults().Count(l.ctx); e != nil {
		return nil, fail(e)
	} else if n > 0 { /* TODO: retain result history or enforce case archival semantics. */
	}
	if e := l.svcCtx.EntClient.EvalCase.DeleteOne(x).Exec(l.ctx); e != nil {
		return nil, fail(e)
	}
	return &types.StatusResp{Status: "deleted"}, nil
}
