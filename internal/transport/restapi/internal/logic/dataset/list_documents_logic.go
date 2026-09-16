// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"

	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListDocumentsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 列出数据集文档
func NewListDocumentsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListDocumentsLogic {
	return &ListDocumentsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListDocumentsLogic) ListDocuments(req *types.DatasetIDReq) (resp *types.DocumentListResp, err error) {
	docs, err := l.svcCtx.Knowledge.List(l.ctx, req.ID)
	if err != nil {
		return nil, documentFail(err)
	}

	data, err := documentDTOs(l.ctx, l.svcCtx.Knowledge, docs)
	if err != nil {
		return nil, documentFail(err)
	}
	return &types.DocumentListResp{Data: data}, nil
}
