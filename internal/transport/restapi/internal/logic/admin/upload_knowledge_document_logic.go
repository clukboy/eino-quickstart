// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package admin

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

func NewUploadKnowledgeDocumentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UploadKnowledgeDocumentLogic {
	return &UploadKnowledgeDocumentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// UploadKnowledgeDocument acknowledges an upload.
//
// Ingestion is not wired in this transport yet; the endpoint keeps the original
// 202 contract (accepted, empty body) so clients can integrate without a shape
// change once ingestion lands. The upload is still rejected when the transport
// was composed without a knowledge store.
func (l *UploadKnowledgeDocumentLogic) UploadKnowledgeDocument(req *types.DatasetIDReq) (resp *types.StatusResp, err error) {
	if _, err = client(l.svcCtx); err != nil {
		return nil, err
	}

	return nil, accepted()
}
