package agent

import (
	"context"
	"eino-quickstart/ent/agentversion"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"entgo.io/ent/dialect/sql"
	"github.com/zeromicro/go-zero/core/logx"
)

type ListAgentVersionsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListAgentVersionsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListAgentVersionsLogic {
	return &ListAgentVersionsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

// ListAgentVersions 列出某个智能体定义的全部版本，**版本号大的在前**，不分页。
//
// 不分页是刻意的（理由见 types.api 的 AgentVersionListResp）：这份数组是详情页的数据源，
// 「提示词与参数」页签要按 def.current_version 找当前版本、版本表的展开区要拿上一版
// 当对比基准、新建版本弹窗要以当前版本为基线 —— 后两者跨行，分页会让它们静默出错。
//
// 倒序是必须的：升序的话「刚发的版本」排在最后，前端 version-panel 的 versions 入参
// 注释本来就写着「倒序（最新在前）」，之前只是没人在后端保证它。
func (l *ListAgentVersionsLogic) ListAgentVersions(req *types.AgentDefIDReq) (*types.AgentVersionListResp, error) {
	rows, err := l.svcCtx.EntClient.AgentVersion.
		Query().
		Where(agentversion.AgentDefID(req.ID)).
		Order(agentversion.ByVersion(sql.OrderDesc())).
		All(l.ctx)
	if err != nil {
		return nil, fail(err)
	}

	out := make([]*types.AgentVersionResp, 0, len(rows))
	for _, row := range rows {
		out = append(out, AgentVersionDTO(row))
	}

	return &types.AgentVersionListResp{Data: out}, nil
}
