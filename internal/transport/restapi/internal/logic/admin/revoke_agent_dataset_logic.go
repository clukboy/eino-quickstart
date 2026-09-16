// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package admin

import (
	"context"

	"eino-quickstart/ent/agentdataset"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type RevokeAgentDatasetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRevokeAgentDatasetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RevokeAgentDatasetLogic {
	return &RevokeAgentDatasetLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// RevokeAgentDataset removes a subject's binding to a dataset.
func (l *RevokeAgentDatasetLogic) RevokeAgentDataset(req *types.AgentDatasetReq) (resp *types.StatusResp, err error) {
	entClient, err := client(l.svcCtx)
	if err != nil {
		return nil, err
	}

	deleted, err := entClient.AgentDataset.Delete().
		Where(
			agentdataset.SubjectEQ(req.Subject),
			agentdataset.DatasetIDEQ(req.ID),
		).
		Exec(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	if deleted == 0 {
		return nil, httpx.NotFound("dataset binding not found")
	}

	return nil, noContent()
}
