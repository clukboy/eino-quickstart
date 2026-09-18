// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"

	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type EnableDocumentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 禁用启用文档
func NewEnableDocumentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *EnableDocumentLogic {
	return &EnableDocumentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *EnableDocumentLogic) EnableDocument(req *types.DocumentIDReq) (resp *types.DocumentResp, err error) {
	doc, err := l.svcCtx.EntClient.Document.Get(l.ctx, req.DocID)
	if err != nil {
		return nil, err
	}

	doc, err = doc.Update().SetEnabled(!doc.Enabled).Save(l.ctx)
	if err != nil {
		return nil, err
	}

	return documentDTOOne(l.ctx, l.svcCtx.Knowledge, doc)
}
