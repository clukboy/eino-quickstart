package eval

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/ent/evalcase"
	"eino-quickstart/ent/evalcaseresult"
	"eino-quickstart/ent/evalrun"
	"eino-quickstart/internal/platform/storage/entx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type DeleteEvalSetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDeleteEvalSetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteEvalSetLogic {
	return &DeleteEvalSetLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *DeleteEvalSetLogic) DeleteEvalSet(req *types.EvalSetIDReq) (*types.StatusResp, error) {
	e := entx.WithTx(l.ctx, l.svcCtx.EntClient, func(tx *ent.Tx) error {
		if _, e := tx.EvalSet.Get(l.ctx, req.ID); e != nil {
			return e
		}
		runs, e := tx.EvalRun.Query().Where(evalrun.EvalSetIDEQ(req.ID)).All(l.ctx)
		if e != nil {
			return e
		}
		for _, r := range runs {
			if _, e = tx.EvalCaseResult.Delete().Where(evalcaseresult.EvalRunIDEQ(r.ID)).Exec(l.ctx); e != nil {
				return e
			}
		}
		if _, e = tx.EvalRun.Delete().Where(evalrun.EvalSetIDEQ(req.ID)).Exec(l.ctx); e != nil {
			return e
		}
		if _, e = tx.EvalCase.Delete().Where(evalcase.EvalSetIDEQ(req.ID)).Exec(l.ctx); e != nil {
			return e
		}
		return tx.EvalSet.DeleteOneID(req.ID).Exec(l.ctx)
	})
	if e != nil {
		return nil, fail(e)
	}
	return &types.StatusResp{Status: "deleted"}, nil
}
