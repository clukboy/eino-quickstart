// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"

	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateDocumentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 更新文档
func NewUpdateDocumentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateDocumentLogic {
	return &UpdateDocumentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// UpdateDocument 改元信息；给了 content 就改写正文并重新索引。
func (l *UpdateDocumentLogic) UpdateDocument(req *types.UpdateDocumentReq) (resp *types.DocumentResp, err error) {
	return
}
