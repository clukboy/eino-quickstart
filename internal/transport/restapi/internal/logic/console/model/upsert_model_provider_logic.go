package model

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/ent/modelprovider"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
	"strings"
)

type UpsertModelProviderLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpsertModelProviderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpsertModelProviderLogic {
	return &UpsertModelProviderLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UpsertModelProviderLogic) UpsertModelProvider(req *types.UpsertModelProviderReq) (*types.ModelProviderResp, error) {
	if strings.TrimSpace(req.Key) == "" || strings.TrimSpace(req.Name) == "" {
		return nil, httpx.BadRequest("provider key and name are required")
	}
	row, e := l.svcCtx.EntClient.ModelProvider.Query().Where(modelprovider.KeyEQ(req.Key)).Only(l.ctx)
	if ent.IsNotFound(e) {
		row, e = l.svcCtx.EntClient.ModelProvider.Create().SetKey(req.Key).SetName(req.Name).SetBaseURL(req.BaseURL).SetAuthType(modelprovider.AuthType(req.AuthType)).SetEnabled(req.Enabled).Save(l.ctx)
	} else if e == nil {
		u := l.svcCtx.EntClient.ModelProvider.UpdateOne(row).SetName(req.Name).SetBaseURL(req.BaseURL).SetAuthType(modelprovider.AuthType(req.AuthType)).SetEnabled(req.Enabled) /* TODO: encrypt req.Secret before persisting; never store plaintext. */
		row, e = u.Save(l.ctx)
	}
	if e != nil {
		return nil, fail(e)
	}
	return providerDTO(row), nil
}
