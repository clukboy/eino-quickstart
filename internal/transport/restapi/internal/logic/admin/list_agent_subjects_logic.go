// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package admin

import (
	"context"
	"sort"
	"strings"

	"eino-quickstart/ent"
	"eino-quickstart/ent/agentdataset"
	"eino-quickstart/ent/user"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListAgentSubjectsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListAgentSubjectsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListAgentSubjectsLogic {
	return &ListAgentSubjectsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListAgentSubjectsLogic) ListAgentSubjects(req *types.AgentListReq) (resp *types.AgentSubjectListResp, err error) {
	entClient, err := client(l.svcCtx)
	if err != nil {
		return nil, err
	}
	page, pageSize, offset, err := normalizePage(req.Page, req.PageSize)
	if err != nil {
		return nil, err
	}
	keyword := strings.TrimSpace(req.Keyword)

	userQuery := entClient.User.Query()
	if keyword != "" {
		userQuery = userQuery.Where(user.Or(
			user.UsernameContainsFold(keyword),
			user.NicknameContainsFold(keyword),
		))
	}
	users, err := userQuery.Order(ent.Desc(user.FieldCreatedAt), ent.Desc(user.FieldID)).All(l.ctx)
	if err != nil {
		return nil, fail(err)
	}

	entries := make(map[string]*types.AgentSubjectResp, len(users))
	for _, record := range users {
		subject := auth.NewAccountSubject(record.ID)
		entries[subject] = &types.AgentSubjectResp{
			Subject:  subject,
			Username: record.Username,
			Nickname: record.Nickname,
			Role:     record.Role.String(),
		}
	}

	bindings, err := entClient.AgentDataset.Query().Order(agentdataset.BySubject()).All(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	for _, binding := range bindings {
		subject := binding.Subject
		if keyword != "" && !strings.Contains(strings.ToLower(subject), strings.ToLower(keyword)) {
			continue
		}
		if _, exists := entries[subject]; !exists {
			entries[subject] = &types.AgentSubjectResp{Subject: subject}
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

func normalizePage(page, pageSize int) (int, int, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	if page > 1_000_000 {
		return 0, 0, 0, httpx.BadRequest("page is too large")
	}
	return page, pageSize, (page - 1) * pageSize, nil
}

func pageSubjects(items []*types.AgentSubjectResp, offset, limit int) []*types.AgentSubjectResp {
	if offset >= len(items) {
		return []*types.AgentSubjectResp{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}
