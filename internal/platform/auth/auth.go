package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
)

type Role string

const (
	RoleAgent    Role = "agent"
	RoleApprover Role = "approver"
	RoleAdmin    Role = "admin"
)

type Identity struct {
	Subject string
	Role    Role
}

type APIKey struct {
	Secret   string
	Identity Identity
}

type storedKey struct {
	fingerprint [sha256.Size]byte
	identity    Identity
}

type Authenticator struct {
	keys []storedKey

	// anonymous 为空表示不签发匿名身份（配置里关掉时组合根不构造 issuer）。
	anonymous *AnonymousIssuer
}

// Option 给 New 追加可选的凭证来源。
type Option func(*Authenticator)

// WithAnonymous 让校验路径也接受匿名令牌（见 anonymous.go）。
//
// 匿名令牌与静态 API key 走**同一个** Bearer 通道，只是判定方式不同：静态
// key 查表、匿名令牌验签。对上层（中间件、role 中间件、logic）来说两者没有
// 区别 —— 都是一个 Identity，所以「用户端用匿名、管理台用 admin key」不需要
// 两条并行的鉴权链路。
func WithAnonymous(issuer *AnonymousIssuer) Option {
	return func(a *Authenticator) {
		a.anonymous = issuer
	}
}

func New(keys []APIKey, opts ...Option) (*Authenticator, error) {
	authenticator := &Authenticator{}
	for _, opt := range opts {
		opt(authenticator)
	}

	// 允许只配匿名（一个静态 key 都没有的部署是合理的：纯用户端），但不允许
	// 两种都没有 —— 那样每个请求都必然 401，问题却要等到线上才暴露。
	if len(keys) == 0 && authenticator.anonymous == nil {
		return nil, errors.New("at least one API key or an anonymous issuer is required")
	}

	stored := make([]storedKey, 0, len(keys))

	for _, key := range keys {
		if key.Secret == "" {
			return nil, errors.New("API key secret is empty")
		}
		if key.Identity.Subject == "" {
			return nil, errors.New("API key subject is empty")
		}
		if key.Identity.Role == "" {
			return nil, errors.New("API key role is empty")
		}

		stored = append(stored, storedKey{
			fingerprint: sha256.Sum256([]byte(key.Secret)),
			identity:    key.Identity,
		})
	}

	authenticator.keys = stored
	return authenticator, nil
}

type identityContextKey struct{}

func IdentityFromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityContextKey{}).(Identity)
	return identity, ok
}

// WithIdentity returns a context carrying identity, using the same private key
// IdentityFromContext reads.
//
// Authenticate uses it internally, and transports that need their own
// rejection contract (for example a transport whose error envelope carries a
// stable code) use it to attach an identity they verified themselves via
// IdentityFor.
func WithIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, identityContextKey{}, identity)
}

func (a *Authenticator) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if header == "" {
			next.ServeHTTP(w, r)
			return
		}

		secret, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || strings.TrimSpace(secret) == "" {
			writeUnauthorized(w)
			return
		}

		identity, ok := a.identityFor(secret)
		if !ok {
			writeUnauthorized(w)
			return
		}

		ctx := WithIdentity(r.Context(), identity)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// identityFor 把 Bearer 里的串还原成身份：先当静态 API key 查表，再当匿名
// 令牌验签。
//
// 顺序不可颠倒也无需担心里程碑：静态 key 是运维写进配置的随机串，与
// `anon.` 前缀的令牌在形状上就不会撞；反过来先验签会让每次带静态 key 的请求
// 都白跑一次 HMAC。
func (a *Authenticator) identityFor(secret string) (Identity, bool) {
	fingerprint := sha256.Sum256([]byte(secret))

	for _, key := range a.keys {
		if subtle.ConstantTimeCompare(
			fingerprint[:],
			key.fingerprint[:],
		) == 1 {
			return key.identity, true
		}
	}

	if a.anonymous != nil {
		return a.anonymous.Verify(secret)
	}

	return Identity{}, false
}

func Require(roles ...Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, ok := IdentityFromContext(r.Context())
			if !ok {
				writeUnauthorized(w)
				return
			}

			for _, role := range roles {
				if identity.Role == RoleAdmin || identity.Role == role {
					next.ServeHTTP(w, r)
					return
				}
			}

			writeForbidden(w)
		})
	}
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
}

func writeForbidden(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":"forbidden"}`))
}
