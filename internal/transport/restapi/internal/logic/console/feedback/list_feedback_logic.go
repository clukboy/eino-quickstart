package feedback

import (
	"context"
	"eino-quickstart/ent/feedback"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type ListFeedbackLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListFeedbackLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListFeedbackLogic {
	return &ListFeedbackLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *ListFeedbackLogic) ListFeedback(req *types.FeedbackListReq) (*types.FeedbackListResp, error) {
	p, s := req.Page, req.PageSize
	if p <= 0 {
		p = 1
	}
	if s <= 0 || s > 200 {
		s = 20
	}
	q := l.svcCtx.EntClient.Feedback.Query()
	if req.Status != "" {
		q.Where(feedback.LabelEQ(req.Status))
	}
	total, e := q.Count(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	rows, e := q.Order(feedback.ByCreatedAt()).Offset((p - 1) * s).Limit(s).All(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	out := make([]*types.FeedbackResp, 0, len(rows))
	for _, x := range rows {
		out = append(out, dto(x))
	}
	return &types.FeedbackListResp{Data: out, Total: total, Page: p, PageSize: s}, nil
}
