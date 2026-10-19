package eval

import (
	"context"
	"eino-quickstart/ent/evalrun"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
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

// ListEvalRuns 列出评测集下的跑批历史，最近一次在前（按开始时间倒序）。
func (l *ListEvalRunsLogic) ListEvalRuns(req *types.EvalRunListReq) (*types.EvalRunListResp, error) {
	page, pageSize := httpx.NormalizePage(req.Page, req.PageSize)

	q := l.svcCtx.EntClient.EvalRun.Query().Where(evalrun.EvalSetIDEQ(req.ID))

	// Count 在加 Order / Offset 之前问：总数与排序、分页都无关。
	total, e := q.Count(l.ctx)
	if e != nil {
		return nil, fail(e)
	}

	rows, e := q.
		Order(evalrun.ByStartedAt(sql.OrderDesc())).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(l.ctx)
	if e != nil {
		return nil, fail(e)
	}

	out := make([]*types.EvalRunResp, 0, len(rows))
	for _, x := range rows {
		out = append(out, runDTO(x))
	}

	// ⚠️ 这里的 Total 是**跑批次数**，不是 EvalRunResp.Total（那个是「这一轮跑了几条
	// 用例」）—— 同名不同义。
	return &types.EvalRunListResp{Data: out, Total: total, Page: page, PageSize: pageSize}, nil
}
