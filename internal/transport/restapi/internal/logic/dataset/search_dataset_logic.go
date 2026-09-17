// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"

	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type SearchDatasetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 数据召回
func NewSearchDatasetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SearchDatasetLogic {
	return &SearchDatasetLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SearchDatasetLogic) SearchDataset(req *types.SearchDatasetReq) (resp *types.DocumentResp, err error) {
	// l.svcCtx.Knowledge.List()

	return
}
