// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"

	"eino-quickstart/ent/document"
	"eino-quickstart/ent/knowledgebase"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListKnowledgeDocumentsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 列出知识文档
func NewListKnowledgeDocumentsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListKnowledgeDocumentsLogic {
	return &ListKnowledgeDocumentsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListKnowledgeDocumentsLogic) ListKnowledgeDocuments(req *types.DatasetIDReq) (resp *types.DocumentListResp, err error) {

	exists, err := l.svcCtx.EntClient.KnowledgeBase.Query().
		Where(knowledgebase.IDEQ(req.ID)).
		Exist(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	if !exists {
		return nil, httpx.NotFound("knowledge base not found")
	}

	docs, err := l.svcCtx.EntClient.Document.Query().
		Where(document.KnowledgeBaseIDEQ(req.ID)).
		Order(document.ByID()).
		All(l.ctx)
	if err != nil {
		return nil, fail(err)
	}

	result := make([]*types.DocumentResp, 0, len(docs))
	for _, doc := range docs {
		dto, err := documentDTO(l.ctx, doc)
		if err != nil {
			return nil, fail(err)
		}
		result = append(result, dto)
	}

	return &types.DocumentListResp{Data: result}, nil
}
