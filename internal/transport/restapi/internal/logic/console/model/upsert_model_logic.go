package model

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/ent/model"
	"eino-quickstart/ent/modelprovider"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
	"strings"
)

type UpsertModelLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpsertModelLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpsertModelLogic {
	return &UpsertModelLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UpsertModelLogic) UpsertModel(req *types.UpsertModelReq) (*types.ModelResp, error) {
	if strings.TrimSpace(req.Key) == "" || req.ProviderKey == "" {
		return nil, httpx.BadRequest("model key and provider_key are required")
	}
	p, e := l.svcCtx.EntClient.ModelProvider.Query().Where(modelprovider.KeyEQ(req.ProviderKey)).Only(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	row, e := l.svcCtx.EntClient.Model.Query().Where(model.KeyEQ(req.Key)).Only(l.ctx)
	if ent.IsNotFound(e) {
		row, e = l.svcCtx.EntClient.Model.Create().SetKey(req.Key).SetProviderID(p.ID).SetProviderKey(req.ProviderKey).SetKind(model.Kind(req.Kind)).SetContextWindow(int(req.ContextWindow)).SetMaxOutputTokens(int(req.MaxOutputTokens)).SetInputPriceMicrosPerK(req.InputPriceMicrosPerK).SetOutputPriceMicrosPerK(req.OutputPriceMicrosPerK).SetEnabled(req.Enabled).Save(l.ctx)
	} else if e == nil {
		row, e = l.svcCtx.EntClient.Model.UpdateOne(row).SetProviderID(p.ID).SetProviderKey(req.ProviderKey).SetKind(model.Kind(req.Kind)).SetContextWindow(int(req.ContextWindow)).SetMaxOutputTokens(int(req.MaxOutputTokens)).SetInputPriceMicrosPerK(req.InputPriceMicrosPerK).SetOutputPriceMicrosPerK(req.OutputPriceMicrosPerK).SetEnabled(req.Enabled).Save(l.ctx)
	}
	if e != nil {
		return nil, fail(e)
	}
	return modelDTO(row), nil
}
