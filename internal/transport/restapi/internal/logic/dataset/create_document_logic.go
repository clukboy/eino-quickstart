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
// 返回的是**列表**：产品型录里的一份正文通常包含好几个产品块，它们会被拆成
// 「一个产品一条文档」。这是唯一会返回多条的情形；其他数据集恒为一条。
//
// 这里**不切块**：每个产品多长、要切几段，要等 worker 内联的 parse 之后才
// 知道，请求内算不出来。所以返回时 embedding 还没开始，status 是 indexing、
// chunk_count 是 0，客户端应轮询 GET 直到 ready 或 failed。每条上的 operation
// 说明这次请求对它做了什么（created / updated / unchanged），只有前两者需要
// 轮询。
func (l *CreateDocumentLogic) CreateDocument(req *types.CreateDocumentReq) (resp *types.DocumentListResp, err error) {
	subject, err := actorSubject(l.ctx)
	if err != nil {
		return nil, err
	}

	results, err := l.svcCtx.Knowledge.Create(l.ctx, knowledge.CreateInput{
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

	list, err := createResultDTOs(l.ctx, l.svcCtx.Knowledge, results)
	if err != nil {
		return nil, documentFail(err)
	}
	return &types.DocumentListResp{Data: list}, nil
}
