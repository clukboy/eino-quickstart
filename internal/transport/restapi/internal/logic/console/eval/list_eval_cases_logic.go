package eval

import (
	"context"
	"eino-quickstart/ent/evalcase"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
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
func (l *ListEvalCasesLogic) ListEvalCases(req *types.EvalSetIDReq) (*types.EvalCaseListResp, error) {
	if _, e := l.svcCtx.EntClient.EvalSet.Get(l.ctx, req.ID); e != nil {
		return nil, fail(e)
	}
	rows, e := l.svcCtx.EntClient.EvalCase.Query().Where(evalcase.EvalSetIDEQ(req.ID)).Order(evalcase.ByCreatedAt()).All(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	out := make([]*types.EvalCaseResp, 0, len(rows))
	for _, x := range rows {
		out = append(out, &types.EvalCaseResp{ID: x.ID, Question: x.Question, ExpectedAnswer: x.ExpectedAnswer, Tags: x.Tags, CreatedAt: x.CreatedAt.UnixMilli()})
	}
	return &types.EvalCaseListResp{Data: out, Total: len(out), Page: 1, PageSize: len(out)}, nil
}
