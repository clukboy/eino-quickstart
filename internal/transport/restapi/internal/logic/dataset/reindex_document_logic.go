// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"

	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ReindexDocumentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 重建文档索引
func NewReindexDocumentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ReindexDocumentLogic {
	return &ReindexDocumentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// ReindexDocument 重新读正文、重新切块、重新排队。
//
// 用途是修复：改了切块参数、换了 embedding 模型、或者上一次索引失败之后，
// 都需要按新配置重建。返回的 DocumentResp 已经是重建后的 chunk_count，
// 但 indexed_chunk_count 要等后台 worker 追上来。
func (l *ReindexDocumentLogic) ReindexDocument(req *types.DocumentIDReq) (resp *types.DocumentResp, err error) {

	// doc, err := service.Reindex(l.ctx, req.ID, req.DocID)
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
