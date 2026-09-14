// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package admin

import (
	"context"

	"eino-quickstart/ent/agentknowledgebase"
	"eino-quickstart/ent/knowledgebase"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GrantAgentKnowledgeBaseLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGrantAgentKnowledgeBaseLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GrantAgentKnowledgeBaseLogic {
	return &GrantAgentKnowledgeBaseLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// GrantAgentKnowledgeBase binds an active knowledge base to a subject. It is
// idempotent: an existing binding is not an error, it is a no-op that still
// answers 204.
func (l *GrantAgentKnowledgeBaseLogic) GrantAgentKnowledgeBase(req *types.AgentKnowledgeBaseReq) (resp *types.StatusResp, err error) {
	entClient, err := client(l.svcCtx)
	if err != nil {
		return nil, err
	}

	identity, ok := auth.IdentityFromContext(l.ctx)
	if !ok {
		return nil, httpx.Unauthorized("unauthorized")
	}

	active, err := entClient.KnowledgeBase.Query().
		Where(knowledgebase.IDEQ(req.ID), knowledgebase.StatusEQ(knowledgebase.StatusACTIVE)).
		Exist(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	if !active {
		return nil, httpx.NotFound("active knowledge base not found")
	}

	bound, err := entClient.AgentKnowledgeBase.Query().
		Where(
			agentknowledgebase.SubjectEQ(req.Subject),
			agentknowledgebase.KnowledgeBaseIDEQ(req.ID),
		).
		Exist(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	if bound {
		return nil, noContent()
	}

	if err = entClient.AgentKnowledgeBase.Create().
		SetSubject(req.Subject).
		SetKnowledgeBaseID(req.ID).
		SetCreatedBy(identity.Subject).
		Exec(l.ctx); err != nil {
		return nil, fail(err)
	}

	return nil, noContent()
}
