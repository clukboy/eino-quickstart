// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"

	"eino-quickstart/internal/application/knowledge"
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

// UpdateDocument 改元信息；给了 content 就改写正文并重新排队索引。
//
// 只有托管目录内的正文允许被覆盖：调用方自己放进 knowledge.root 的文件是它
// 的资产，接口不该无声改写 —— 那种情况返回 400，而不是偷偷越过。
func (l *UpdateDocumentLogic) UpdateDocument(req *types.UpdateDocumentReq) (resp *types.DocumentResp, err error) {
	doc, err := l.svcCtx.Knowledge.Update(l.ctx, knowledge.UpdateInput{
		DatasetID:  req.ID,
		DocumentID: req.DocID,
		Title:      req.Title,
		Content:    req.Content,
		Visibility: req.Visibility,
		Metadata:   req.Metadata,
	})
	if err != nil {
		return nil, documentFail(err)
	}

	resp, err = documentDTOOne(l.ctx, l.svcCtx.Knowledge, doc)
	if err != nil {
		return nil, documentFail(err)
	}
	return resp, nil
}
