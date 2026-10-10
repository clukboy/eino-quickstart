package tool

import (
	"context"
	enttool "eino-quickstart/ent/tool"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
	"strings"
)

type UpdateToolLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateToolLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateToolLogic {
	return &UpdateToolLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UpdateToolLogic) UpdateTool(req *types.UpdateToolReq) (*types.ToolResp, error) {
	row, err := l.svcCtx.EntClient.Tool.Query().Where(enttool.KeyEQ(req.Key)).Only(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	u := l.svcCtx.EntClient.Tool.UpdateOne(row)
	if strings.TrimSpace(req.Name) != "" {
		u.SetName(strings.TrimSpace(req.Name))
	}
	if req.Description != "" {
		u.SetDescription(req.Description)
	}
	if req.RiskTier != "" {
		u.SetRiskTier(req.RiskTier)
	}
	if req.AuthType != "" {
		a, e := parseAuth(req.AuthType)
		if e != nil {
			return nil, httpx.BadRequest("invalid tool auth_type")
		}
		u.SetAuthType(a)
	}
	if req.TimeoutMS > 0 {
		u.SetTimeoutMs(int(req.TimeoutMS))
	}
	// TODO: encrypt req.Secret before persisting; an empty secret must keep the existing one.
	row, err = u.Save(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	return dto(row), nil
}
