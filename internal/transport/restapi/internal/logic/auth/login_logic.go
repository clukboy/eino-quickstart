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

const invalidCredentialsMessage = "用户名或密码不正确"

type LoginLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LoginLogic {
	return &LoginLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *LoginLogic) Login(req *types.LoginReq) (resp *types.AuthTokenResp, err error) {
	store, issuer, err := accountDeps(l.svcCtx)
	if err != nil {
		return nil, err
	}

	if req.Username == "" || req.Password == "" {
		return nil, httpx.BadRequest("用户名和密码不能为空")
	}

	credentials, err := store.ByUsername(l.ctx, req.Username)
	switch {
	case errors.Is(err, account.ErrNotFound):
		account.BurnPasswordCheck(l.svcCtx.PasswordHashCost)
		return nil, httpx.InvalidCredentials(invalidCredentialsMessage)
	case err != nil:
		l.Errorf("look up account by username failed: %v", err)
		return nil, httpx.Internal("登录失败")
	}

	if !account.CheckPassword(credentials.PasswordHash, req.Password) {
		return nil, httpx.InvalidCredentials(invalidCredentialsMessage)
	}
	if !credentials.Active() {
		return nil, httpx.InvalidCredentials(invalidCredentialsMessage)
	}

	subject := platformauth.NewAccountSubject(credentials.ID)
	role := platformauth.Role(credentials.Role)

	token, expiresAt, err := issuer.IssueFor(subject, role, credentials.MustChangePassword)
	if err != nil {
		l.Errorf("issue account token failed subject=%s role=%s: %v", subject, role, err)
		return nil, httpx.Internal("登录失败")
	}

	migrated := l.migrateAnonymousHistory(store, req.AnonymousToken, subject)

	// 不记密码、不记令牌：前者是管理员设定的共享凭证，后者等同于身份本身。
	l.Infof(
		"account login subject=%s username=%s must_change_password=%t migrated_sessions=%d",
		subject,
		credentials.Username,
		credentials.MustChangePassword,
		migrated,
	)

	return tokenResponse(
		token,
		credentials,
		time.Now(),
		expiresAt,
	), nil
}

// migrateAnonymousHistory 把登录前攒下的匿名会话并到账号名下，返回迁移条数。
//
// ── 为什么失败不阻断登录 ──────────────────────────────────────────
// 迁移是附带好处，而失败的代价如果算在登录上就是「用户以后再也登不进来」——
// 一个过期的匿名令牌不该有这个后果。所以所有异常路径都只记日志、返回 0。
//
// ── 为什么必须先验签 ──────────────────────────────────────────────
// 客户端传什么 subject 就迁什么，等于让它把**别人的**会话搬到自己的新账号
// 名下 —— 一次成功的越权读取。所以 subject 只能取自服务端验过的令牌，绝不能
// 从请求体里直接读。
//
// 用 context.WithoutCancel：迁移是登录请求的**副作用**，但它的效果必须留下。
// 若客户端在收到响应前断开，请求 context 会被取消，一条已经开始的 UPDATE 可能
// 半途而废，而用户下次登录时那个匿名令牌多半已经过期、再也迁不成 —— 攒下的
// 历史就永久留在 anon: 名下了。
func (l *LoginLogic) migrateAnonymousHistory(
	store *account.Store,
	token string,
	to string,
) int64 {
	if token == "" {
		return 0
	}

	anonymous := l.svcCtx.Anonymous
	if anonymous == nil {
		// 这个部署没开匿名身份，那么这个令牌本来也不可能是本服务签的。
		l.Infof("skip anonymous history migration: anonymous identity is disabled")
		return 0
	}

	identity, ok := anonymous.Verify(token)
	if !ok {
		l.Infof(
			"skip anonymous history migration: anonymous token did not verify subject=%s",
			to,
		)
		return 0
	}
	if identity.Subject == to {
		return 0
	}

	migrated, err := store.MigrateAnonymousHistory(
		context.WithoutCancel(l.ctx),
		identity.Subject,
		to,
	)
	if err != nil {
		l.Errorf(
			"migrate anonymous history failed from=%s to=%s: %v",
			identity.Subject,
			to,
			err,
		)
		return 0
	}

	return migrated
}
