package tool

import (
	"context"
	enttool "eino-quickstart/ent/tool"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type ListToolsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListToolsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListToolsLogic {
	return &ListToolsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *ListToolsLogic) ListTools(req *types.ToolListReq) (*types.ToolListResp, error) {
	page, size := req.Page, req.PageSize
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 200 {
		size = 20
	}
	q := l.svcCtx.EntClient.Tool.Query()
	if req.Keyword != "" {
		q.Where(enttool.NameContainsFold(req.Keyword))
	}
	if req.Kind != "" {
		k, err := parseKind(req.Kind)
		if err != nil {
			return nil, httpx.BadRequest("invalid tool kind")
		}
		q.Where(enttool.KindEQ(k))
	}
	if req.RiskTier != "" {
		q.Where(enttool.RiskTierEQ(req.RiskTier))
	}
	if req.Enabled == "true" {
		q.Where(enttool.EnabledEQ(true))
	}
	if req.Enabled == "false" {
		q.Where(enttool.EnabledEQ(false))
	}
	total, err := q.Count(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	rows, err := q.Order(enttool.ByUpdatedAt()).Offset((page - 1) * size).Limit(size).All(l.ctx)
	if err != nil {
		return nil, fail(err)
	}
	data := make([]*types.ToolResp, 0, len(rows))
	for _, row := range rows {
		data = append(data, dto(row))
	}
	return &types.ToolListResp{Data: data, Total: total, Page: page, PageSize: size}, nil
}
