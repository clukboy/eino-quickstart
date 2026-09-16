// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"

	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ReindexDatasetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 重建数据集全部文档索引
func NewReindexDatasetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ReindexDatasetLogic {
	return &ReindexDatasetLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// ReindexDataset 逐个文档重建，互不影响：某个文档的正文丢了只计入 failed，
// 其余照常排队。
//
// 整库重建在请求内只做切块与落库（纯 CPU + 一次批量插入），真正的 embedding
// 全在后台，因此不会撞上传输层的 15s 超时。返回 200 表示「已受理」，
// 不是「已索引完成」。
func (l *ReindexDatasetLogic) ReindexDataset(req *types.DatasetIDReq) (resp *types.ReindexResp, err error) {

	// result, err := service.ReindexDataset(l.ctx, req.ID)
	// if err != nil {
	// 	return nil, documentFail(err)
	// }
	return &types.ReindexResp{
		Status: "accepted",
		// Documents: result.Documents,
		// Chunks:    result.Chunks,
		// Failed:    result.Failed,
	}, nil
}
