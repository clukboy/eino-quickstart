package feedback

import (
	"context"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type LabelFeedbackLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLabelFeedbackLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LabelFeedbackLogic {
	return &LabelFeedbackLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *LabelFeedbackLogic) LabelFeedback(req *types.LabelFeedbackReq) (*types.FeedbackResp, error) {
	if req.Label == "" {
		return nil, httpx.BadRequest("label is required")
	}
	x, e := l.svcCtx.EntClient.Feedback.Get(l.ctx, req.ID)
	if e != nil {
		return nil, fail(e)
	}
	u := l.svcCtx.EntClient.Feedback.UpdateOne(x).SetLabel(req.Label)
	if req.Note != "" {
		u.SetNote(req.Note)
	}
	x, e = u.Save(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	return dto(x), nil
}
