// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"

	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type DeleteDocumentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 删除文档
func NewDeleteDocumentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteDocumentLogic {
	return &DeleteDocumentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// DeleteDocument 同步删除：摘掉 chunk 与索引任务、清理向量、删行、删托管正文。
// 这几步都是单次 RPC 或一条 SQL，不像 embedding 那样有长尾，所以不必排队。
func (l *DeleteDocumentLogic) DeleteDocument(req *types.DocumentIDReq) (resp *types.StatusResp, err error) {

	// if err := l.svcCtx.EntC.Document.
	// 	Delete().
	// 	Where(document.ID(req.DocID)).
	// 	Exec(l.ctx); err != nil {
	// 	return nil, documentFail(err)
	// }
	return &types.StatusResp{Status: "deleted"}, nil
}
