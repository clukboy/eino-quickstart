// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"

	"eino-quickstart/internal/application/knowledge"
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

// CreateDocument 落正文、写文档行、投递索引任务。
//
// 这里**不切块**：一份文件里有几个产品、每篇正文多长，要等 worker 内联的
// parse 之后才知道，请求内算不出来。所以返回时 embedding 还没开始，status
// 是 indexing、chunk_count 是 0，客户端应轮询 GET 直到 ready 或 failed。
func (l *CreateDocumentLogic) CreateDocument(req *types.CreateDocumentReq) (resp *types.DocumentResp, err error) {
	subject, err := actorSubject(l.ctx)
	if err != nil {
		return nil, err
	}

	doc, err := l.svcCtx.Knowledge.Create(l.ctx, knowledge.CreateInput{
		DatasetID:    req.ID,
		Title:        req.Title,
		Content:      req.Content,
		Source:       req.Source,
		Visibility:   req.Visibility,
		OwnerSubject: subject,
		Metadata:     req.Metadata,
	})
	if err != nil {
		return nil, documentFail(err)
	}

	resp, err = documentDTOOne(l.ctx, l.svcCtx.Knowledge, doc)
	if err != nil {
		return nil, documentFail(err)
	}
	return resp, nil
}
