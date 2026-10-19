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

type ListDocumentsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 列出数据集文档（分页）
func NewListDocumentsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListDocumentsLogic {
	return &ListDocumentsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListDocumentsLogic) ListDocuments(req *types.DocumentListReq) (resp *types.DocumentListResp, err error) {
	page, pageSize := httpx.NormalizePage(req.Page, req.PageSize)

	docs, total, err := l.svcCtx.Knowledge.ListPage(l.ctx, req.ID, page, pageSize)
	if err != nil {
		return nil, documentFail(err)
	}

	data, err := documentDTOs(l.ctx, l.svcCtx.Knowledge, docs)
	if err != nil {
		return nil, documentFail(err)
	}

	// Total 是整库文档数，不是本页条数：前端据它算总页数。
	return &types.DocumentListResp{Data: data, Total: total, Page: page, PageSize: pageSize}, nil
}
