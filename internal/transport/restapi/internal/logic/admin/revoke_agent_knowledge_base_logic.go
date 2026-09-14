// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package admin

import (
	"context"

	"eino-quickstart/ent/agentknowledgebase"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type RevokeAgentKnowledgeBaseLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRevokeAgentKnowledgeBaseLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RevokeAgentKnowledgeBaseLogic {
	return &RevokeAgentKnowledgeBaseLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// RevokeAgentKnowledgeBase removes a subject's binding to a knowledge base.
func (l *RevokeAgentKnowledgeBaseLogic) RevokeAgentKnowledgeBase(req *types.AgentKnowledgeBaseReq) (resp *types.StatusResp, err error) {
	entClient, err := client(l.svcCtx)
	if err != nil {
		return nil, err
	}

	deleted, err := entClient.AgentKnowledgeBase.Delete().
		Where(
			agentknowledgebase.SubjectEQ(req.Subject),
			agentknowledgebase.KnowledgeBaseIDEQ(req.ID),
		).
		Exec(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	if deleted == 0 {
		return nil, httpx.NotFound("knowledge base binding not found")
	}

	return nil, noContent()
}
