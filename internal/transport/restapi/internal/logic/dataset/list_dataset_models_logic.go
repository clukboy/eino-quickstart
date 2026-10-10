package dataset

import (
	"context"

	"eino-quickstart/ent/model"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type ListDatasetModelsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListDatasetModelsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListDatasetModelsLogic {
	return &ListDatasetModelsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *ListDatasetModelsLogic) ListDatasetModels() (*types.DatasetModelListResp, error) {
	rows, err := l.svcCtx.EntClient.Model.Query().Where(model.EnabledEQ(true)).All(l.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*types.DatasetModelOption, 0, len(rows))
	for _, row := range rows {
		out = append(out, &types.DatasetModelOption{Value: row.Key, Label: row.Key, Kind: string(row.Kind)})
	}

	return &types.DatasetModelListResp{Data: out}, nil
}
