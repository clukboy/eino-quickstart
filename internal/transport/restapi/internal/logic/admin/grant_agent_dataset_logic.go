// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package admin

import (
	"context"

	"eino-quickstart/ent/agentdataset"
	"eino-quickstart/ent/dataset"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GrantAgentDatasetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGrantAgentDatasetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GrantAgentDatasetLogic {
	return &GrantAgentDatasetLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// GrantAgentDataset binds an active dataset to a subject. It is idempotent: an
// existing binding is not an error, it is a no-op that still answers 204.
func (l *GrantAgentDatasetLogic) GrantAgentDataset(req *types.AgentDatasetReq) (resp *types.StatusResp, err error) {
	entClient, err := client(l.svcCtx)
	if err != nil {
		return nil, err
	}

	identity, ok := auth.IdentityFromContext(l.ctx)
	if !ok {
		return nil, httpx.Unauthorized("unauthorized")
	}

	active, err := entClient.Dataset.Query().
		Where(dataset.IDEQ(req.ID), dataset.StatusEQ(dataset.StatusACTIVE)).
		Exist(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	if !active {
		return nil, httpx.NotFound("active dataset not found")
	}

	bound, err := entClient.AgentDataset.Query().
		Where(
			agentdataset.SubjectEQ(req.Subject),
			agentdataset.DatasetIDEQ(req.ID),
		).
		Exist(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	if bound {
		return nil, noContent()
	}

	if err = entClient.AgentDataset.Create().
		SetSubject(req.Subject).
		SetDatasetID(req.ID).
		SetCreatedBy(identity.Subject).
		Exec(l.ctx); err != nil {
		return nil, fail(err)
	}

	return nil, noContent()
}
