// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"

	"eino-quickstart/internal/application/knowledge"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type SearchDatasetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 数据召回
func NewSearchDatasetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SearchDatasetLogic {
	return &SearchDatasetLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SearchDatasetLogic) SearchDataset(req *types.SearchDatasetReq) (resp *types.SearchResp, err error) {
	outcome, err := l.svcCtx.Knowledge.Search(l.ctx, knowledge.SearchInput{
		DatasetID: req.DatasetID,
		Query:     req.Query,
		TopK:      req.TopK,
	})
	if err != nil {
		return nil, documentFail(err)
	}

	resp = &types.SearchResp{
		Data:          make([]*types.SearchHitResp, 0, len(outcome.Hits)),
		Channels:      outcome.Channels,
		Degraded:      outcome.Degraded,
		TopK:          outcome.TopK,
		Granularity:   string(outcome.Granularity),
		MatchedChunks: outcome.MatchedChunks,
		ChunkBudget:   outcome.ChunkBudget,
	}
	for _, hit := range outcome.Hits {
		resp.Data = append(resp.Data, &types.SearchHitResp{
			ChunkID:          hit.ChunkID,
			DocumentID:       hit.DocumentID,
			Source:           hit.Source,
			Title:            hit.Title,
			HeadingPath:      hit.HeadingPath,
			Content:          hit.Content,
			ContentTruncated: hit.Truncated,
			Score:            hit.Score,
		})
	}
	return resp, nil
}
