package run

import (
	"context"
	"eino-quickstart/ent/agentrun"
	"eino-quickstart/ent/runretrievalhit"
	"eino-quickstart/ent/runspan"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type GetRunLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetRunLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetRunLogic {
	return &GetRunLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *GetRunLogic) GetRun(req *types.RunPathReq) (*types.RunDetailResp, error) {
	x, e := l.svcCtx.EntClient.AgentRun.Query().Where(agentrun.RunIDEQ(req.RunID)).Only(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	spans, e := l.svcCtx.EntClient.RunSpan.Query().Where(runspan.RunIDEQ(req.RunID)).Order(runspan.ByStartedAt()).All(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	spanOut := make([]*types.RunSpanResp, 0, len(spans))
	for _, s := range spans {
		spanOut = append(spanOut, spanDTO(s))
	}
	hits, e := l.svcCtx.EntClient.RunRetrievalHit.Query().Where(runretrievalhit.RunIDEQ(req.RunID)).Order(runretrievalhit.ByRank()).All(l.ctx)
	if e != nil {
		return nil, fail(e)
	}
	_ = hits
	return &types.RunDetailResp{Run: runDTO(x), Spans: spanOut, Hits: make([]*types.SearchHitResp, 0), Messages: make([]*types.RunMessageResp, 0)}, nil /* TODO: map retrieval hits and session messages to their complete frontend projections. */
}
