// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"

	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetDocumentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 获取文档详情
func NewGetDocumentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetDocumentLogic {
	return &GetDocumentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetDocumentLogic) GetDocument(req *types.DocumentIDReq) (resp *types.DocumentResp, err error) {
	// service, err := documents(l.svcCtx)
	// if err != nil {
	// 	return nil, err
	// }

	// doc, err := service.Get(l.ctx, req.ID, req.DocID)
	// if err != nil {
	// 	return nil, documentFail(err)
	// }

	// resp, err = documentDTOOne(l.ctx, service, doc)
	// if err != nil {
	// 	return nil, documentFail(err)
	// }
	// return resp, nil
	return
}
