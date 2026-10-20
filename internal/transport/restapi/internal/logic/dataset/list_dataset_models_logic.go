package dataset

import (
	"context"

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

// ListDatasetModels 返回可用于知识库的模型。
//
// 模型不再存库：服务器部署只认 config.yaml 的 Embedding 段，所以这里直接回
// 配置里的那个 embedding 模型。保留这个端点是为了让建库表单仍能拿到选项，
// 而不是让前端在删掉模型管理后拿不到值。
func (l *ListDatasetModelsLogic) ListDatasetModels() (*types.DatasetModelListResp, error) {
	name := l.svcCtx.EmbeddingModel
	out := make([]*types.DatasetModelOption, 0, 1)
	if name != "" {
		out = append(out, &types.DatasetModelOption{
			Value: name,
			Label: name,
			Kind:  "embedding",
		})
	}

	return &types.DatasetModelListResp{Data: out}, nil
}
