package eval

import (
	"context"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
	"strings"
)

type CreateEvalSetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateEvalSetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateEvalSetLogic {
	return &CreateEvalSetLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *CreateEvalSetLogic) CreateEvalSet(req *types.CreateEvalSetReq) (*types.EvalSetResp, error) {
	id, ok := auth.IdentityFromContext(l.ctx)
	if !ok {
		return nil, httpx.Unauthorized("unauthorized")
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, httpx.BadRequest("evaluation set name is required")
	}
	b := l.svcCtx.EntClient.EvalSet.Create().SetName(strings.TrimSpace(req.Name)).SetDescription(req.Description).SetCreatedBy(id.Subject)
	if req.AgentKey != "" {
		b.SetAgentKey(req.AgentKey)
	}
	x, e := b.Save(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	return setDTO(x, 0, nil), nil
}
