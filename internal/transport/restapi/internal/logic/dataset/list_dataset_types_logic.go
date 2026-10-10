package dataset

import (
	"context"

	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type ListDatasetTypesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListDatasetTypesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListDatasetTypesLogic {
	return &ListDatasetTypesLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *ListDatasetTypesLogic) ListDatasetTypes() (*types.DatasetTypeListResp, error) {
	return &types.DatasetTypeListResp{Data: []*types.DatasetTypeOption{{Value: "default", Label: "Default"}, {Value: "product", Label: "Product"}}}, nil
}
