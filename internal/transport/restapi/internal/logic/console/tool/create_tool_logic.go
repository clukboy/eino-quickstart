package tool

import (
	"context"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
	"strings"
)

type CreateToolLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateToolLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateToolLogic {
	return &CreateToolLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *CreateToolLogic) CreateTool(req *types.CreateToolReq) (*types.ToolResp, error) {
	key, name := strings.TrimSpace(req.Key), strings.TrimSpace(req.Name)
	if key == "" || name == "" {
		return nil, httpx.BadRequest("tool key and name are required")
	}
	kind, err := parseKind(req.Kind)
	if err != nil {
		return nil, httpx.BadRequest("invalid tool kind")
	}
	authType, err := parseAuth(req.AuthType)
	if err != nil {
		return nil, httpx.BadRequest("invalid tool auth_type")
	}
	b := l.svcCtx.EntClient.Tool.Create().SetKey(key).SetName(name).SetKind(kind).SetDescription(req.Description).SetRiskTier(req.RiskTier).SetAuthType(authType).SetTimeoutMs(int(req.TimeoutMS))
	if req.TimeoutMS <= 0 {
		b.SetTimeoutMs(30000)
	}
	// TODO: encrypt req.Secret before persisting; never store plaintext.
	row, err := b.Save(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	return dto(row), nil
}
