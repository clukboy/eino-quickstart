// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"

	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type DeleteDatasetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 删除知识库
func NewDeleteDatasetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteDatasetLogic {
	return &DeleteDatasetLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteDatasetLogic) DeleteDataset(req *types.DatasetIDReq) (resp *types.StatusResp, err error) {
	err = l.svcCtx.EntClient.Dataset.DeleteOneID(req.ID).Exec(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	resp = &types.StatusResp{
		Status: "deleted",
	}

	return
}
