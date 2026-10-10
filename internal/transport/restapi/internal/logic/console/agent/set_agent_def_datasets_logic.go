package agent

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/ent/agentdefdataset"
	"eino-quickstart/ent/dataset"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/storage/entx"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type SetAgentDefDatasetsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSetAgentDefDatasetsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SetAgentDefDatasetsLogic {
	return &SetAgentDefDatasetsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *SetAgentDefDatasetsLogic) SetAgentDefDatasets(req *types.SetAgentDefDatasetsReq) (*types.StatusResp, error) {
	identity, ok := auth.IdentityFromContext(l.ctx)
	if !ok {
		return nil, httpx.Unauthorized("unauthorized")
	}
	err := entx.WithTx(l.ctx, l.svcCtx.EntClient, func(tx *ent.Tx) error {
		if _, e := tx.AgentDef.Get(l.ctx, req.ID); e != nil {
			return e
		}
		if _, e := tx.Dataset.Query().Where(dataset.IDIn(req.DatasetIDs...)).All(l.ctx); e != nil {
			return e
		}
		if _, e := tx.AgentDefDataset.Delete().Where(agentdefdataset.AgentDefID(req.ID)).Exec(l.ctx); e != nil {
			return e
		}
		for _, id := range req.DatasetIDs {
			if _, e := tx.AgentDefDataset.Create().SetAgentDefID(req.ID).SetDatasetID(id).SetCreatedBy(identity.Subject).Save(l.ctx); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return nil, fail(err)
	}
	return &types.StatusResp{Status: "updated"}, nil
}
