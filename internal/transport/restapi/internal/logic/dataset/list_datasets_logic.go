// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"
	"strings"

	"eino-quickstart/ent/dataset"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListDatasetsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 列出知识库（分页）
func NewListDatasetsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListDatasetsLogic {
	return &ListDatasetsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListDatasetsLogic) ListDatasets(req *types.ListDatasetsReq) (resp *types.DatasetListResp, err error) {
	page, pageSize := httpx.NormalizePage(req.Page, req.PageSize)

	query := l.svcCtx.EntClient.Dataset.Query()

	// 过滤必须在 Count **之前**加：Count 和 All 共用同一个 query，
	// 漏了一处就会出现「共 47 条、这一页 3 条、点下一页还是 3 条」这种对不上的分页。
	// 空串 = 不筛（契约里两个都是 optional，缺席时解析成零值）。
	if name := strings.TrimSpace(req.Name); name != "" {
		query = query.Where(dataset.NameContainsFold(name))
	}
	if kind := strings.TrimSpace(req.Type); kind != "" {
		query = query.Where(dataset.TypeEQ(kind))
	}

	total, err := query.Count(l.ctx)
	if err != nil {
		return nil, fail(err)
	}

	// 按 id 升序：created_at 只精确到毫秒，同时建的两个库会并列，并列时顺序由数据库定 —— 翻页会重复或丢行
	bases, err := query.
		Order(dataset.ByID()).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(l.ctx)
	if err != nil {
		return nil, fail(err)
	}

	result := make([]*types.DatasetResp, 0, len(bases))
	for _, base := range bases {
		result = append(result, DatasetDTO(base))
	}

	// Total 是**过滤后**的命中总数，不是本页条数也不是库里的总数：前端据它算总页数。
	return &types.DatasetListResp{Data: result, Total: total, Page: page, PageSize: pageSize}, nil
}
