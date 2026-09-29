// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package user

import (
	"context"

	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type SyncUsersLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 从外部数据源同步账号（占位：当前恒返回 501 not_implemented）
func NewSyncUsersLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SyncUsersLogic {
	return &SyncUsersLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// SyncUsers 是**占位实现**：它恒定返回 501，不做任何事。
//
// ── 为什么要把一个空接口做出来 ────────────────────────────────────
// 管理后台的用户列表上有一个「同步」按钮，数据源（从哪个系统拉人、用什么字段
// 匹配、覆盖哪些属性）还没定。先把路由、中间件、错误码与前端调用打通，是为了
// 让「按钮点了没反应」与「按钮点了报 501 并说明原因」成为两种可区分的情形 ——
// 后者是诚实的状态，前者会被当成前端 bug 查半天。
//
// ── 为什么是 501 而不是降到「返回 200 + 空结果」 ──────────────────
// 200 会让管理员相信同步成功、只是暂时没有新账号，而实际上一个都没拉。501 是
// HTTP 里语义完全对应的码（Not Implemented），并且与 503（本部署关掉了这个
// 功能）区分开：前者是「还没做」，后者是「不提供」。
//
// 前端把这个 501 的文案当**提示**展示（不是红色报错），见
// src/api/user.ts 与 views/users/users.vue。
//
// ── 真接入时要做什么 ──────────────────────────────────────────────
//  1. 定数据源与匹配键，把结果形状定下来（created/updated/skipped…），并同步
//     修改 docs/user/user.api 的 returns（现在是借用的 StatusResp）。
//  2. 同步只应该**新增与更新**，永远不能因为上游少了一个人就把本地账号删掉 ——
//     那会让那个人攒下的所有会话与审计记录失去归属。
//  3. 拉进来的人同样要走「必须改密」：他们的初始密码要么由上游提供（不可信），
//     要么由本地随机生成（那就需要一个下发渠道）。这件事比接口形状重要得多。
func (l *SyncUsersLogic) SyncUsers() (resp *types.StatusResp, err error) {
	l.Infof("user sync requested but the upstream data source is not wired yet")

	return nil, httpx.NotImplemented("用户同步数据源尚未接入")
}
