// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"

	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetDatasetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 获取知识库详情
func NewGetDatasetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetDatasetLogic {
	return &GetDatasetLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetDatasetLogic) GetDataset(req *types.DatasetIDReq) (resp *types.DatasetResp, err error) {
	base, err := l.svcCtx.EntClient.KnowledgeBase.Get(l.ctx, req.ID)
	if err != nil {
		return nil, fail(err)
	}

	return DatasetDTO(base), nil
}
