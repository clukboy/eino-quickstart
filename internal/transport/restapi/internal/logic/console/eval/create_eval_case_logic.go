package eval

import (
	"context"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type CreateEvalCaseLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateEvalCaseLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateEvalCaseLogic {
	return &CreateEvalCaseLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *CreateEvalCaseLogic) CreateEvalCase(req *types.CreateEvalCaseReq) (*types.EvalCaseResp, error) {
	if req.Question == "" {
		return nil, httpx.BadRequest("question is required")
	}
	if _, e := l.svcCtx.EntClient.EvalSet.Get(l.ctx, req.ID); e != nil {
		return nil, fail(e)
	}
	x, e := l.svcCtx.EntClient.EvalCase.Create().SetEvalSetID(req.ID).SetQuestion(req.Question).SetExpectedAnswer(req.ExpectedAnswer).SetTags(req.Tags).Save(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	return &types.EvalCaseResp{ID: x.ID, Question: x.Question, ExpectedAnswer: x.ExpectedAnswer, Tags: x.Tags, CreatedAt: x.CreatedAt.UnixMilli()}, nil
}
