// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"

	"eino-quickstart/ent/dataset"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListDatasetsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 列出知识库
func NewListDatasetsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListDatasetsLogic {
	return &ListDatasetsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListDatasetsLogic) ListDatasets(req *types.ListDatasetsReq) (resp *types.DatasetListResp, err error) {
	bases, err := l.svcCtx.EntClient.Dataset.Query().Order(dataset.ByID()).All(l.ctx)
	if err != nil {
		return nil, fail(err)
	}

	result := make([]*types.DatasetResp, 0, len(bases))
	for _, base := range bases {
		result = append(result, DatasetDTO(base))
	}

	return &types.DatasetListResp{Data: result}, nil
}
