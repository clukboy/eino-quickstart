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

// GetDocument 是「索引进度」的查询入口：status 从 indexing 变 ready、以及
// indexed_chunk_count 逐批追上 chunk_count，都只能从这里看到。
func (l *GetDocumentLogic) GetDocument(req *types.DocumentIDReq) (resp *types.DocumentResp, err error) {
	doc, err := l.svcCtx.Knowledge.Get(l.ctx, req.ID, req.DocID)
	if err != nil {
		return nil, documentFail(err)
	}

	resp, err = documentDTOOne(l.ctx, l.svcCtx.Knowledge, doc)
	if err != nil {
		return nil, documentFail(err)
	}
	return resp, nil
}
