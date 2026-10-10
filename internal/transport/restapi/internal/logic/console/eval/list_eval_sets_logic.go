package eval

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/ent/evalrun"
	"eino-quickstart/ent/evalset"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"entgo.io/ent/dialect/sql"
	"github.com/zeromicro/go-zero/core/logx"
)

type ListEvalSetsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListEvalSetsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListEvalSetsLogic {
	return &ListEvalSetsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *ListEvalSetsLogic) ListEvalSets(req *types.EvalSetListReq) (*types.EvalSetListResp, error) {
	p, s := req.Page, req.PageSize
	if p <= 0 {
		p = 1
	}
	if s <= 0 || s > 200 {
		s = 20
	}
	q := l.svcCtx.EntClient.EvalSet.Query()
	if req.Keyword != "" {
		q.Where(evalset.NameContainsFold(req.Keyword))
	}
	total, e := q.Count(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	rows, e := q.Order(evalset.ByCreatedAt()).Offset((p - 1) * s).Limit(s).All(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	out := make([]*types.EvalSetResp, 0, len(rows))
	for _, x := range rows {
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
		out = append(out, setDTO(x, n, last))
	}
	return &types.EvalSetListResp{Data: out, Total: total, Page: p, PageSize: s}, nil
}
