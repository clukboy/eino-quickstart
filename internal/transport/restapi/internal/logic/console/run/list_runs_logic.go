package run

import (
	"context"
	"time"

	"eino-quickstart/ent"
	"eino-quickstart/ent/chatturn"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListRunsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListRunsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListRunsLogic {
	return &ListRunsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

// ListRuns 按时间倒序列出对话轮次。
//
// 数据源是 chat_turns（一轮一行）。早先读的 agent_runs 与它逐列重复，已经删除。
// req.AgentKey 不再参与过滤：只有一个 agent，按 key 筛等于全选；保留字段是为了
// 不动前端契约。
func (l *ListRunsLogic) ListRuns(req *types.RunListReq) (*types.RunListResp, error) {
	page, pageSize := req.Page, req.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}

	query := l.svcCtx.EntClient.ChatTurn.Query()
	if req.Subject != "" {
		query.Where(chatturn.OwnerSubjectEQ(req.Subject))
	}
	if req.Status != "" {
		query.Where(chatturn.StatusEQ(chatturn.Status(req.Status)))
	}
	if req.FromMS > 0 {
		query.Where(chatturn.CreatedAtGTE(time.UnixMilli(req.FromMS)))
	}
	if req.ToMS > 0 {
		query.Where(chatturn.CreatedAtLTE(time.UnixMilli(req.ToMS)))
	}

	total, err := query.Count(l.ctx)
	if err != nil {
		return nil, fail(err)
	}

	turns, err := query.
		Order(ent.Desc(chatturn.FieldCreatedAt)).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(l.ctx)
	if err != nil {
		return nil, fail(err)
	}

	data := make([]*types.RunResp, 0, len(turns))
	for _, turn := range turns {
		data = append(data, turnDTO(turn))
	}

	return &types.RunListResp{Data: data, Total: total, Page: page, PageSize: pageSize}, nil
}
