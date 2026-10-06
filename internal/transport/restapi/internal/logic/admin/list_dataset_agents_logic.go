// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package admin

import (
	"context"
	"sort"

	"eino-quickstart/ent/agentdataset"
	"eino-quickstart/ent/dataset"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListDatasetAgentsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListDatasetAgentsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListDatasetAgentsLogic {
	return &ListDatasetAgentsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListDatasetAgentsLogic) ListDatasetAgents(req *types.DatasetAgentsReq) (resp *types.AgentSubjectListResp, err error) {
	entClient, err := client(l.svcCtx)
	if err != nil {
		return nil, err
	}
	page, pageSize, offset, err := normalizePage(req.Page, req.PageSize)
	if err != nil {
		return nil, err
	}
	exists, err := entClient.Dataset.Query().Where(dataset.IDEQ(req.ID)).Exist(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	if !exists {
		return nil, httpx.NotFound("dataset not found")
	}

	bindings, err := entClient.AgentDataset.Query().
		Where(agentdataset.DatasetIDEQ(req.ID)).
		Order(agentdataset.BySubject()).
		All(l.ctx)
	if err != nil {
		return nil, fail(err)
	}

	entries := make(map[string]*types.AgentSubjectResp, len(bindings))
	for _, binding := range bindings {
		entries[binding.Subject] = &types.AgentSubjectResp{Subject: binding.Subject}
	}

	users, err := entClient.User.Query().All(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	for _, record := range users {
		subject := auth.NewAccountSubject(record.ID)
		if item, ok := entries[subject]; ok {
			item.Username = record.Username
			item.Nickname = record.Nickname
			item.Role = record.Role.String()
		}
	}

	items := make([]*types.AgentSubjectResp, 0, len(entries))
	for _, item := range entries {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Subject < items[j].Subject })
	return &types.AgentSubjectListResp{
		Data:     pageSubjects(items, offset, pageSize),
		Total:    len(items),
		Page:     page,
		PageSize: pageSize,
	}, nil
}
