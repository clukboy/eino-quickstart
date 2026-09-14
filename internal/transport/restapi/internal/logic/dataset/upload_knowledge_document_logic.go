// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"

	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UploadKnowledgeDocumentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 上传知识文档
func NewUploadKnowledgeDocumentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UploadKnowledgeDocumentLogic {
	return &UploadKnowledgeDocumentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UploadKnowledgeDocumentLogic) UploadKnowledgeDocument(req *types.DatasetIDReq) (resp *types.StatusResp, err error) {
	// todo: add your logic here and delete this line

	return
}
