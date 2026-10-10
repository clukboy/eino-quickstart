package usage

import (
	"context"
	"time"

	"eino-quickstart/ent/agentrun"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type GetUsageLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetUsageLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUsageLogic {
	return &GetUsageLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

type usageGroup struct {
	key, name     string
	runs          int
	in, out, cost int64
}

func (l *GetUsageLogic) GetUsage(req *types.UsageReq) (*types.UsageResp, error) {
	from := time.Now().Add(-7 * 24 * time.Hour)
	if req.FromMS > 0 {
		from = time.UnixMilli(req.FromMS)
	}
	q := l.svcCtx.EntClient.AgentRun.Query().Where(agentrun.CreatedAtGTE(from))
	if req.ToMS > 0 {
		q.Where(agentrun.CreatedAtLTE(time.UnixMilli(req.ToMS)))
	}
	rows, err := q.All(l.ctx)
	if err != nil {
		return nil, err
	}
	totals := &types.UsageTotalsResp{}
	groups := map[string]*usageGroup{}
	trend := map[int64]*types.UsagePointResp{}
	loc := time.FixedZone("CST", 8*3600)
	for _, r := range rows {
		totals.Runs++
		totals.TokensIn += r.TokensIn
		totals.TokensOut += r.TokensOut
		totals.CostMicros += r.CostMicros
		key, name := r.AgentKey, r.AgentKey
		switch req.GroupBy {
		case "subject":
			key, name = r.RequestedBy, r.RequestedBy
		case "agent":
			key, name = r.AgentKey, r.AgentKey
		case "model", "dataset":
			// TODO: model/dataset dimensions require execution metadata not yet persisted on AgentRun.
			key, name = "", ""
		}
		if groups[key] == nil {
			groups[key] = &usageGroup{key: key, name: name}
		}
		g := groups[key]
		g.runs++
		g.in += r.TokensIn
		g.out += r.TokensOut
		g.cost += r.CostMicros
		t := r.CreatedAt.In(loc)
		if req.Interval == "hour" {
			t = t.Truncate(time.Hour)
		} else {
			y, m, d := t.Date()
			t = time.Date(y, m, d, 0, 0, 0, 0, loc)
		}
		ms := t.UnixMilli()
		if trend[ms] == nil {
			trend[ms] = &types.UsagePointResp{Bucket: t.Format(time.RFC3339), BucketStartMS: ms}
		}
		trend[ms].Runs++
		trend[ms].Tokens += r.TokensIn + r.TokensOut
		trend[ms].CostMicros += r.CostMicros
	}
	rowsOut := make([]*types.UsageRowResp, 0, len(groups))
	for _, g := range groups {
		rowsOut = append(rowsOut, &types.UsageRowResp{Key: g.key, Name: g.name, Runs: g.runs, TokensIn: g.in, TokensOut: g.out, CostMicros: g.cost})
	}
	points := make([]*types.UsagePointResp, 0, len(trend))
	for _, p := range trend {
		points = append(points, p)
	}
	return &types.UsageResp{Totals: totals, Trend: points, Rows: rowsOut}, nil // TODO: sort trend/rows deterministically.
}
