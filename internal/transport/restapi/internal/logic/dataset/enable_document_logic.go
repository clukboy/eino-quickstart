// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"

	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type EnableDocumentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 明确设置文档是否参与召回；重复请求不会翻转状态。
func NewEnableDocumentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *EnableDocumentLogic {
	return &EnableDocumentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *EnableDocumentLogic) EnableDocument(req *types.DocumentEnabledReq) (resp *types.DocumentResp, err error) {
	if req.Enabled == nil {
		return nil, httpx.BadRequest("enabled is required")
	}

	doc, err := l.svcCtx.Knowledge.SetEnabled(l.ctx, req.ID, req.DocID, *req.Enabled)
	if err != nil {
		return nil, documentFail(err)
	}

	resp, err = documentDTOOne(l.ctx, l.svcCtx.Knowledge, doc)
	if err != nil {
		return nil, documentFail(err)
	}
	return resp, nil
}
