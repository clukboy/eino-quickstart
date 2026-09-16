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

// ReindexDocument 重新读正文、重新排队。用途是修复：改了切块参数、换了
// embedding 模型、或者上一次索引失败之后，都按新配置重建。
//
// 返回的 DocumentResp 里 chunk_count / indexed_chunk_count 还是重建前的存量：
// 切块要等 worker 拿到任务之后才发生，请求内给不出新值，客户端要轮询 GET
// 看 indexed_chunk_count 追上来。
func (l *ReindexDocumentLogic) ReindexDocument(req *types.DocumentIDReq) (resp *types.DocumentResp, err error) {
	doc, err := l.svcCtx.Knowledge.Reindex(l.ctx, req.ID, req.DocID)
	if err != nil {
		return nil, documentFail(err)
	}

	resp, err = documentDTOOne(l.ctx, l.svcCtx.Knowledge, doc)
	if err != nil {
		return nil, documentFail(err)
	}
	return resp, nil
}
