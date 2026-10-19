package eval

import (
	"context"
	"eino-quickstart/ent/evalcase"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"entgo.io/ent/dialect/sql"
	"github.com/zeromicro/go-zero/core/logx"
)

type ListEvalCasesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListEvalCasesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListEvalCasesLogic {
	return &ListEvalCasesLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

// ListEvalCases 列出评测集下的用例，**新加的在前**（按 ID 倒序，稳定同 created_at）。
//
// 用例是只增不删地往里加的，加上分页之后必须倒序：升序的话第 1 页永远是最早那批，
// 刚刚补的用例要翻到最后一页才看得见。
func (l *ListEvalCasesLogic) ListEvalCases(req *types.EvalCaseListReq) (*types.EvalCaseListResp, error) {
	// 先确认评测集存在：不存在回 404 而不是空列表，否则「集选错了」与
	// 「这个集还没建用例」在界面上长得一模一样。
	if _, e := l.svcCtx.EntClient.EvalSet.Get(l.ctx, req.ID); e != nil {
		return nil, fail(e)
	}

	page, pageSize := httpx.NormalizePage(req.Page, req.PageSize)

	q := l.svcCtx.EntClient.EvalCase.Query().Where(evalcase.EvalSetIDEQ(req.ID))

	// Count 在加 Order / Offset 之前问：总数与排序、分页都无关。
	total, e := q.Count(l.ctx)
	if e != nil {
		return nil, fail(e)
	}

	rows, e := q.
		Order(evalcase.ByID(sql.OrderDesc())).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(l.ctx)
	if e != nil {
		return nil, fail(e)
	}

	out := make([]*types.EvalCaseResp, 0, len(rows))
	for _, x := range rows {
		out = append(out, &types.EvalCaseResp{ID: x.ID, Question: x.Question, ExpectedAnswer: x.ExpectedAnswer, Tags: x.Tags, CreatedAt: x.CreatedAt.UnixMilli()})
	}

	// Total 是集内用例总数，不是本页条数：前端据它算总页数。
	return &types.EvalCaseListResp{Data: out, Total: total, Page: page, PageSize: pageSize}, nil
}
