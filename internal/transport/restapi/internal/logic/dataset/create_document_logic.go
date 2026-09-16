// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"

	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateDocumentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 创建文档
func NewCreateDocumentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateDocumentLogic {
	return &CreateDocumentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// CreateDocument 落正文、写文档行、切块入队。返回时 embedding 还没开始，
// 所以 status 是 indexing，客户端应轮询 GET 直到 ready 或 failed。
func (l *CreateDocumentLogic) CreateDocument(req *types.CreateDocumentReq) (resp *types.DocumentResp, err error) {

	// doc, err := service.Create(l.ctx, knowledge.CreateInput{
	// 	DatasetID:  req.ID,
	// 	Title:      req.Title,
	// 	Content:    req.Content,
	// 	Source:     req.Source,
	// 	Visibility: req.Visibility,
	// 	Metadata:   req.Metadata,
	// })
	// if err != nil {
	// 	return nil, documentFail(err)
	// }

	// resp, err = documentDTOOne(l.ctx, service, doc)
	// if err != nil {
	// 	return nil, documentFail(err)
	// }
	return resp, nil
}
