// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"
	"strings"

	"eino-quickstart/ent/dataset"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateDatasetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 创建知识库
func NewCreateDatasetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateDatasetLogic {
	return &CreateDatasetLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateDatasetLogic) CreateDataset(req *types.CreateDatasetReq) (resp *types.DatasetResp, err error) {

	identity, ok := auth.IdentityFromContext(l.ctx)
	if !ok {
		return nil, httpx.Unauthorized("unauthorized")
	}

	name := strings.TrimSpace(req.Name)
	visibility := strings.ToLower(strings.TrimSpace(req.Visibility))
	if name == "" {
		return nil, httpx.BadRequest("knowledge base name is required")
	}
	if visibility == "" {
		visibility = string(dataset.VisibilityPrivate)
	}
	if visibility != string(dataset.VisibilityPrivate) &&
		visibility != string(dataset.VisibilitySystem) {
		return nil, httpx.BadRequest("knowledge base visibility is invalid")
	}

	base, err := l.svcCtx.EntClient.Dataset.Create().
		SetName(name).
		SetDescription(strings.TrimSpace(req.Description)).
		SetOwnerSubject(identity.Subject).
		SetVisibility(dataset.Visibility(visibility)).
		SetStatus(dataset.DefaultStatus).
		SetType(strings.TrimSpace(req.Type)).
		Save(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	return DatasetDTO(base), nil
}
