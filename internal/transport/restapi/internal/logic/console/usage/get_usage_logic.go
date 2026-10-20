package usage

import (
	"context"
	"time"

	"eino-quickstart/ent/chatturn"
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
	key, name string
	turns     int
}

// GetUsage 按维度统计最近一段时间的对话轮次。
//
// 数据源是 chat_turns。token 与成本字段保留在响应里但恒为 0 —— 这一版没有
// 采集用量，宁可为空也不编造（早先读的 agent_runs 里那几列从未写入过）。
//
// group_by=agent 已经没有意义（只有一个 agent），按同一主体聚合；model/dataset
// 同理没有落库维度，返回空 key 由前端显示「未分类」。
func (l *GetUsageLogic) GetUsage(req *types.UsageReq) (*types.UsageResp, error) {
	from := time.Now().Add(-7 * 24 * time.Hour)
	if req.FromMS > 0 {
		from = time.UnixMilli(req.FromMS)
	}

	query := l.svcCtx.EntClient.ChatTurn.Query().Where(chatturn.CreatedAtGTE(from))
	if req.ToMS > 0 {
		query.Where(chatturn.CreatedAtLTE(time.UnixMilli(req.ToMS)))
	}

	turns, err := query.All(l.ctx)
	if err != nil {
		return nil, err
	}

	totals := &types.UsageTotalsResp{}
	groups := map[string]*usageGroup{}
	trend := map[int64]*types.UsagePointResp{}
	loc := time.FixedZone("CST", 8*3600)

	for _, turn := range turns {
		totals.Runs++

		key, name := turn.OwnerSubject, turn.OwnerSubject
		switch req.GroupBy {
		case "subject":
			key, name = turn.OwnerSubject, turn.OwnerSubject
		case "agent", "model", "dataset":
			key, name = "", ""
		}

		if groups[key] == nil {
			groups[key] = &usageGroup{key: key, name: name}
		}
		groups[key].turns++

		at := turn.CreatedAt.In(loc)
		if req.Interval == "hour" {
			at = at.Truncate(time.Hour)
		} else {
			y, m, d := at.Date()
			at = time.Date(y, m, d, 0, 0, 0, 0, loc)
		}

		bucket := at.UnixMilli()
		if trend[bucket] == nil {
			trend[bucket] = &types.UsagePointResp{Bucket: at.Format(time.RFC3339), BucketStartMS: bucket}
		}
		trend[bucket].Runs++
	}

	rows := make([]*types.UsageRowResp, 0, len(groups))
	for _, group := range groups {
		rows = append(rows, &types.UsageRowResp{Key: group.key, Name: group.name, Runs: group.turns})
	}

	points := make([]*types.UsagePointResp, 0, len(trend))
	for _, point := range trend {
		points = append(points, point)
	}

	return &types.UsageResp{Totals: totals, Trend: points, Rows: rows}, nil
}
