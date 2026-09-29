// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package auth

import (
	"context"
	"errors"
	"time"

	platformauth "eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/persistence/account"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ChangePasswordLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 修改当前账号的密码（成功后返回新令牌，无需重新登录）
func NewChangePasswordLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ChangePasswordLogic {
	return &ChangePasswordLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// ChangePassword 换掉当前账号的密码，并返回一枚新令牌。
//
// ── 为什么要返回令牌 ──────────────────────────────────────────────
// must_change_password 签在令牌载荷里（tokenPayload.mcp），服务端不查库，
// 所以改完库不改令牌的话，手上这枚旧令牌仍然带着「待改密」，下一次请求照样被
// 密码守卫 403 —— 而用户刚刚改完，界面上的 /auth/me 却可能说他已经改过了。
// 那是一个自相矛盾且无解的状态。直接换一枚（mcp=false）就消除了它，顺带省掉
// 「改完密码请重新登录」这一步。
//
// ── 为什么还要再要一次当前密码 ────────────────────────────────────
// 请求已经带了有效令牌（第一个因素），再要一次当前密码是为了「拿到一枚令牌
// 不等于能永久霸占这个账号」—— 例如有人趁电脑没锁屏时改掉密码，把真正的用户
// 锁在外面。改密是一个不可逆、且会影响到别人登录的动作，值得双因素。
func (l *ChangePasswordLogic) ChangePassword(
	req *types.ChangePasswordReq,
) (resp *types.AuthTokenResp, err error) {
	store, issuer, err := accountDeps(l.svcCtx)
	if err != nil {
		return nil, err
	}

	// 静态 API key 与匿名令牌都会在这里被挡掉：它们的 subject 不是 user:<id>，
	// 也就没有密码可改。用「身份长得对不对」判定，而不是挂角色中间件 ——
	// 后者会误伤另外两种角色（这是本组不挂中间件的理由之一）。
	id, ok := currentAccountID(l.ctx)
	if !ok {
		return nil, httpx.Unauthorized("需要登录账号")
	}

	credentials, err := store.CredentialsByID(l.ctx, id)
	switch {
	case errors.Is(err, account.ErrNotFound):
		// 令牌验签通过但账号已经不在了（被删、或库被重置）。回 401 而不是 404：
		// 对客户端来说这与「没登录」没有任何区别，而 404 会暗示一个它无从处理的
		// 概念。
		return nil, httpx.Unauthorized("需要登录账号")
	case err != nil:
		l.Errorf("look up account by id failed subject=%s: %v", l.subject(), err)
		return nil, httpx.Internal("修改密码失败")
	}

	if !account.CheckPassword(credentials.PasswordHash, req.CurrentPassword) {
		// 400 而不是 401。这一条很关键：前端的 401 自愈逻辑会清掉登录态
		// （src/api/auth.ts 里的 onUnauthorized → resetIdentity），用 401 表示
		// 「当前密码打错了」会把用户莫名其妙地登出。令牌本身是好的，
		// 错的是请求体。
		return nil, httpx.BadRequest("当前密码不正确")
	}

	if err := account.ValidatePassword(req.NewPassword); err != nil {
		return nil, httpx.BadRequest(err.Error())
	}

	// 新密码不能与当前密码相同。这不是洁癖：管理员设定的初始密码**管理员本人
	// 知道**，若允许「改成同一个值」，用户点一下就把 must_change_password
	// 清掉了，而那枚共享密码继续有效 —— 「首次强制改密」于是完全失效，且界面上
	// 看起来一切正常。
	if account.CheckPassword(credentials.PasswordHash, req.NewPassword) {
		return nil, httpx.BadRequest("新密码不能与当前密码相同")
	}

	newHash, err := account.HashPassword(req.NewPassword, l.svcCtx.PasswordHashCost)
	if err != nil {
		l.Errorf("hash password failed subject=%s: %v", l.subject(), err)
		return nil, httpx.Internal("修改密码失败")
	}

	if err := store.ChangePassword(l.ctx, id, newHash); err != nil {
		if errors.Is(err, account.ErrNotFound) {
			return nil, httpx.Unauthorized("需要登录账号")
		}
		l.Errorf("update password failed subject=%s: %v", l.subject(), err)
		return nil, httpx.Internal("修改密码失败")
	}

	subject := platformauth.NewAccountSubject(id)
	role := platformauth.Role(credentials.Role)

	token, expiresAt, err := issuer.IssueFor(subject, role, false)
	if err != nil {
		// 密码**已经改成了**，只是新令牌没签出来。回 500 是唯一诚实的选择：
		// 用户需要重新登录一次（旧令牌仍是 mcp=true，会被守卫拦住），但至少
		// 密码是对的。文案必须写清楚，否则支持人员会以为是改密失败。
		l.Errorf(
			"password changed but issuing the new token failed subject=%s role=%s: %v",
			subject, role, err,
		)
		return nil, httpx.Internal("密码已修改，但签发新令牌失败，请重新登录")
	}

	// 不记密码、不记令牌。
	l.Infof("password changed subject=%s username=%s", subject, credentials.Username)

	return tokenResponse(
		token,
		subject,
		role,
		credentials.Username,
		false,
		time.Now(),
		expiresAt,
	), nil
}

// subject 只用于日志，让「谁在反复试当前密码」这类问题在日志里可查。
func (l *ChangePasswordLogic) subject() string {
	if identity, ok := platformauth.IdentityFromContext(l.ctx); ok {
		return identity.Subject
	}

	return ""
}
