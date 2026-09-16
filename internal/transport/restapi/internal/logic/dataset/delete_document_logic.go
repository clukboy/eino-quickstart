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

// DeleteDocument 同步删除：摘掉分块、删行、清理向量、删托管正文。
//
// 这几步都是单次 RPC 或一条 SQL，不像 embedding 那样有长尾，所以不必排队。
// 已经在排队的索引任务不用取消：worker 领到任务时发现文档已经不在了，会静默
// 跳过；如果在处理途中被删，写回状态的那一步会绕开已删除的行。
func (l *DeleteDocumentLogic) DeleteDocument(req *types.DocumentIDReq) (resp *types.StatusResp, err error) {
	if err := l.svcCtx.Knowledge.Delete(l.ctx, req.ID, req.DocID); err != nil {
		return nil, documentFail(err)
	}
	return &types.StatusResp{Status: "deleted"}, nil
}
