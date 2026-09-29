// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package auth

import (
	"context"
	"errors"

	platformauth "eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/persistence/account"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetMeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 查询当前身份（区分登录账号与匿名访客）
func NewGetMeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetMeLogic {
	return &GetMeLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// GetMe 回答「当前这个令牌是谁」。
//
// 这是被密码守卫显式放行的两个接口之一（另一个是 /auth/password），而它必须
// 被放行：前端要靠它判断「登录了没有、要不要先改密」，如果它也被 403 拦住，
// 用户就永远不知道该去改密。所以这里的 401 口径要准 —— 它是前端决定「重新
// 申请匿名身份」还是「跳去登录」的依据。
func (l *GetMeLogic) GetMe() (resp *types.AuthMeResp, err error) {
	identity, ok := platformauth.IdentityFromContext(l.ctx)
	if !ok || identity.Subject == "" {
		// 没有身份回 401，而不是「authenticated=false 的 200」：前端要把
		// 「根本没有令牌」与「令牌有效但我没登录（匿名访客）」分开，前者要先去
		// 申请一个身份，后者不用。两个都回 200 的话这个区别就没了。
		return nil, httpx.Unauthorized("unauthenticated")
	}

	// 账号身份：回库补全用户名与创建时间（令牌里只有 subject 和角色）。
	if id, ok := platformauth.ParseAccountSubject(identity.Subject); ok {
		accountStore, _, depsErr := accountDeps(l.svcCtx)
		if depsErr != nil {
			return nil, depsErr
		}

		record, lookupErr := accountStore.ByID(l.ctx, id)
		switch {
		case errors.Is(lookupErr, account.ErrNotFound):
			// 令牌验签通过但账号已经不在了。与 change_password 同一口径：
			// 对客户端来说这与「没登录」没有区别。
			return nil, httpx.Unauthorized("需要登录账号")
		case lookupErr != nil:
			l.Errorf("look up account by id failed subject=%s: %v", identity.Subject, lookupErr)
			return nil, httpx.Internal("查询账号失败")
		}

		return &types.AuthMeResp{
			Authenticated: true,
			Subject:       identity.Subject,
			Username:      record.Username,
			// 用**令牌里**的角色，不是 record.Role。两者在「登录后管理员改了库」
			// 时会不一致，而实际生效的是令牌里那个 —— 角色中间件读的就是
			// identity。这个接口若回 record.Role，界面会显示「你现在是 admin」
			// 而每个管理接口仍然 403，那种矛盾比不显示更难排查。
			Role:               string(identity.Role),
			MustChangePassword: identity.MustChangePassword,
			CreatedAt:          record.CreatedAt.UnixMilli(),
		}, nil
	}

	// 匿名访客与静态 API key：有角色，但不存在「账号」这一层。
	//
	// 两者都回 authenticated=false 是刻意的：对用户端来说「我没登录」才是它
	// 需要知道的事实，而「当前其实是一个 admin key」是管理台的事，不该由一个
	// 客户端可能并未持有的令牌告知。
	return &types.AuthMeResp{
		Authenticated: false,
		Subject:       identity.Subject,
		Role:          string(identity.Role),
		// 从令牌里取而不是写死 false：匿名与静态 key 恒为假，但取真实值能让
		// 「令牌与身份不一致」这一类 bug 在测试里露出来，而不是被写死的默认值
		// 掩盖。
		MustChangePassword: identity.MustChangePassword,
	}, nil
}
