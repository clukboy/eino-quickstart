package eval

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/ent/evalrun"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"entgo.io/ent/dialect/sql"
	"github.com/zeromicro/go-zero/core/logx"
)

type GetEvalSetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetEvalSetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetEvalSetLogic {
	return &GetEvalSetLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *GetEvalSetLogic) GetEvalSet(req *types.EvalSetIDReq) (*types.EvalSetResp, error) {
	x, e := l.svcCtx.EntClient.EvalSet.Get(l.ctx, req.ID)
	if e != nil {
		return nil, fail(e)
	}
	n, e := x.QueryCases().Count(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	last, e := x.QueryRuns().Order(evalrun.ByStartedAt(sql.OrderDesc())).First(l.ctx)
	if e != nil && !ent.IsNotFound(e) {
		return nil, fail(e)
	}
	if ent.IsNotFound(e) {
		last = nil
	}
	return setDTO(x, n, last), nil
}
