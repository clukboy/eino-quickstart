// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"

	"eino-quickstart/ent/document"
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
	docs, err := l.svcCtx.EntClient.Document.Query().Where(document.DatasetIDEQ(req.ID)).All(l.ctx)
	if err != nil {
		return nil, documentFail(err)
	}

	data, err := documentDTOs(l.ctx, docs)
	if err != nil {
		return nil, documentFail(err)
	}
	return &types.DocumentListResp{Data: data}, nil
}
