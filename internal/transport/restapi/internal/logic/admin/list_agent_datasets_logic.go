// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package admin

import (
	"context"

	"eino-quickstart/ent/agentdataset"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListAgentDatasetsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListAgentDatasetsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListAgentDatasetsLogic {
	return &ListAgentDatasetsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// ListAgentDatasets lists the datasets bound to a subject.
func (l *ListAgentDatasetsLogic) ListAgentDatasets(req *types.AgentSubjectReq) (resp *types.DatasetListResp, err error) {
	entClient, err := client(l.svcCtx)
	if err != nil {
		return nil, err
	}

	bindings, err := entClient.AgentDataset.Query().
		Where(agentdataset.SubjectEQ(req.Subject)).
		Order(agentdataset.ByDatasetID()).
		WithDataset().
		All(l.ctx)
	if err != nil {
		return nil, fail(err)
	}

	result := make([]*types.DatasetResp, 0, len(bindings))
	for _, binding := range bindings {
		base, err := binding.Edges.DatasetOrErr()
		if err != nil {
			return nil, fail(err)
		}
		result = append(result, DatasetDTO(base))
	}

	return &types.DatasetListResp{Data: result}, nil
}
