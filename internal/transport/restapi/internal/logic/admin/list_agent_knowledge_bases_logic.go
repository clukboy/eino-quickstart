// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package admin

import (
	"context"

	"eino-quickstart/ent/agentknowledgebase"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListAgentKnowledgeBasesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListAgentKnowledgeBasesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListAgentKnowledgeBasesLogic {
	return &ListAgentKnowledgeBasesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// ListAgentKnowledgeBases lists the knowledge bases bound to a subject.
func (l *ListAgentKnowledgeBasesLogic) ListAgentKnowledgeBases(req *types.AgentSubjectReq) (resp *types.DatasetListResp, err error) {
	entClient, err := client(l.svcCtx)
	if err != nil {
		return nil, err
	}

	bindings, err := entClient.AgentKnowledgeBase.Query().
		Where(agentknowledgebase.SubjectEQ(req.Subject)).
		Order(agentknowledgebase.ByKnowledgeBaseID()).
		WithKnowledgeBase().
		All(l.ctx)
	if err != nil {
		return nil, fail(err)
	}

	result := make([]*types.DatasetResp, 0, len(bindings))
	for _, binding := range bindings {
		base, err := binding.Edges.KnowledgeBaseOrErr()
		if err != nil {
			return nil, fail(err)
		}
		result = append(result, DatasetDTO(base))
	}

	return &types.DatasetListResp{Data: result}, nil
}
