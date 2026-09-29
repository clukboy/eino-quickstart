package auth

import (
	"context"
	"time"

	platformauth "eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/persistence/account"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
)

// accountDeps 取账号功能的两半，未开启时回 503。
//
// 两个都要判：restapi.New 已经保证「要么都有、要么都没有」，但逻辑层不该依赖
// 上游的构造期校验 —— 那样一个空指针会变成 500（panic 被 Recover 兜住）而不是
// 一句「账号登录未开启」。503 与 /auth/anonymous 在没有 issuer 时的口径一致。
//
// 注意**不做** users 表的其他兜底：没有「退回默认管理员」这种降级，那等于给
// 所有人开一个后门。功能没开就是没开。
func accountDeps(svcCtx *svc.ServiceContext) (*account.Store, *platformauth.AccountIssuer, error) {
	store := svcCtx.AccountStore
	issuer := svcCtx.Accounts
	if store == nil || issuer == nil {
		return nil, nil, httpx.Unavailable("账号登录未开启")
	}

	return store, issuer, nil
}

// tokenResponse 把签发结果装配成两个接口共用的响应类型。
func tokenResponse(
	token string,
	subject string,
	role platformauth.Role,
	username string,
	mustChangePassword bool,
	issuedAt time.Time,
	expiresAt time.Time,
) *types.AuthTokenResp {
	return &types.AuthTokenResp{
		Token:              token,
		Subject:            subject,
		Role:               string(role),
		Username:           username,
		MustChangePassword: mustChangePassword,
		IssuedAt:           issuedAt.UnixMilli(),
		ExpiresAt:          expiresAt.UnixMilli(),
	}
}

// currentAccountID 从请求上下文里取当前登录账号的 id。
//
// 静态 API key 与匿名令牌都会被这里拒掉（它们的 subject 不是 user:<id>）——
// 这正是 /auth/me 与 /auth/password 不挂角色中间件之后需要自己补的那一环：
// 改用「身份长得对不对」判定，比挂任一角色中间件都准（那会误伤另外两种角色）。
func currentAccountID(ctx context.Context) (uint64, bool) {
	identity, ok := platformauth.IdentityFromContext(ctx)
	if !ok {
		return 0, false
	}

	return platformauth.ParseAccountSubject(identity.Subject)
}
