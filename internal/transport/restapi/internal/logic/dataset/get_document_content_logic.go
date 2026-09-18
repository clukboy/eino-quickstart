// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"

	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetDocumentContentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 获取文档内容
func NewGetDocumentContentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetDocumentContentLogic {
	return &GetDocumentContentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetDocumentContentLogic) GetDocumentContent(req *types.DocumentIDReq) (resp *types.DocumentContentResp, err error) {
	doc, err := l.svcCtx.Knowledge.Get(l.ctx, req.ID, req.DocID)
	if err != nil {
		return nil, err
	}
	resp, err = documentContentOne(l.ctx, l.svcCtx.Knowledge, doc)
	if err != nil {
		return nil, err
	}

	return
}
