// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package admin

import (
	"context"

	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetKnowledgeBaseLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetKnowledgeBaseLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetKnowledgeBaseLogic {
	return &GetKnowledgeBaseLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// GetKnowledgeBase returns one knowledge base.
func (l *GetKnowledgeBaseLogic) GetKnowledgeBase(req *types.DatasetIDReq) (resp *types.DatasetResp, err error) {
	entClient, err := client(l.svcCtx)
	if err != nil {
		return nil, err
	}

	base, err := entClient.KnowledgeBase.Get(l.ctx, req.ID)
	if err != nil {
		return nil, fail(err)
	}

	return DatasetDTO(base), nil
}
