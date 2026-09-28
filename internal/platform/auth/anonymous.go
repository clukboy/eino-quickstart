package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AnonymousSubjectPrefix 是匿名身份的 subject 前缀。
//
// 归属都写成 `<类型>:<id>` 而不是裸 id，是为了让「这条会话是谁的」在库里
// 一眼可读、并且日后能可靠地做迁移：接入账号登录之后要给「把匿名历史并到
// 账号名下」写一条 UPDATE，而 sessions.owner_subject 是 Immutable（创建后
// 改不了，见 ent/schema/session.go），只能靠裸 SQL 按前缀筛出来。
const AnonymousSubjectPrefix = "anon:"

// 令牌前缀。与匿名 subject 的前缀刻意不同名：一个是「这个令牌是匿名类型的」，
// 一个是「这个身份是匿名的」，前者用于快速分流校验路径，后者是归属字符串的
// 第一个字段。
const anonymousTokenPrefix = "anon."

// tokenPayload 是令牌里自包含的载荷。
//
// 字段名取短名（s / exp）：这个串会出现在每个请求的 Authorization 头里，
// 它只对签发方与校验方有意义，可读性由本文件的注释负责。
type tokenPayload struct {
	Subject   string `json:"s"`
	ExpiresAt int64  `json:"exp"`
}

// AnonymousIssuer 签发并校验匿名身份令牌。
//
// ── 为什么是「服务端签发」而不是「客户端自报 id」 ──────────────────────
// 匿名身份必须能隔离会话（A 看不到 B 的），而隔离的前提是 subject 无法被
// 客户端随意指定。若让前端生成一个 UUID 放在请求头里，任何人改一下那个头
// 就能读写别人的会话 —— 匿名场景下这就是唯一的安全边界，不能交给客户端。
// 于是 subject 由服务端生成，并连同过期时间一起签名，客户端只负责**保管**
// 这个令牌，改任何一个字节都会验签失败。
//
// ── 为什么无状态 ──────────────────────────────────────────────────
// 令牌自包含（HMAC-SHA256），服务端不存匿名用户，因此不需要新表、不需要
// 每个请求查一次库。代价是**签出去就收不回**（没有服务端吊销），以及无法
// 统计「现在有多少匿名身份」—— 匿名身份本来就不是账号，两者都不影响隔离。
//
// ── 日后接入账号登录 ───────────────────────────────────────────────
// 那时只需再实现一种签发（subject 用另一个前缀，例如 `user:`），校验路径
// 与业务侧（actorSubject、会话归属、角色中间件）一行都不用改；已签发的匿名
// 令牌继续有效，用户在登录前攒下的历史仍归在 anon: 名下。
type AnonymousIssuer struct {
	secret []byte
	ttl    time.Duration
	role   Role
	prefix string

	// now 可注入，只为让「过期」这条分支在测试里能确定性地触发。
	now func() time.Time
}

// AnonymousConfig 是构造 AnonymousIssuer 的输入。
//
// Secret 为空即视为「不签发匿名身份」，由调用方（组合根）决定要不要构造，
// 所以这里不做「空 secret 也放行」的降级 —— 一个空密钥等于任何人都能伪造
// 任意 subject 的令牌。
type AnonymousConfig struct {
	Secret        string
	TTL           time.Duration
	Role          Role
	SubjectPrefix string
}

func NewAnonymousIssuer(cfg AnonymousConfig) (*AnonymousIssuer, error) {
	if cfg.Secret == "" {
		return nil, errors.New("auth: anonymous secret is required")
	}
	if cfg.TTL <= 0 {
		return nil, errors.New("auth: anonymous token ttl must be greater than zero")
	}
	if cfg.Role == "" {
		cfg.Role = RoleAgent
	}
	switch cfg.Role {
	case RoleAgent, RoleApprover, RoleAdmin:
	default:
		return nil, fmt.Errorf("auth: anonymous role %q is not a known role", cfg.Role)
	}
	if cfg.SubjectPrefix == "" {
		cfg.SubjectPrefix = AnonymousSubjectPrefix
	}

	return &AnonymousIssuer{
		secret: []byte(cfg.Secret),
		ttl:    cfg.TTL,
		role:   cfg.Role,
		prefix: cfg.SubjectPrefix,
		now:    time.Now,
	}, nil
}

// Role 是匿名身份被授予的角色，供签发接口回传给客户端（前端据此说明
// 「当前是访客，只能访问对话相关接口」）。
func (i *AnonymousIssuer) Role() Role { return i.role }

// SubjectPrefix 是匿名 subject 的前缀，语义同 AnonymousSubjectPrefix。
func (i *AnonymousIssuer) SubjectPrefix() string { return i.prefix }

// NewAnonymousSubject 生成一个新的匿名 subject。
func NewAnonymousSubject(prefix string) string {
	if prefix == "" {
		prefix = AnonymousSubjectPrefix
	}
	return prefix + uuid.NewString()
}

// Issue 为 subject 签发一个令牌；subject 为空时新建一个匿名身份。
//
// 传入 subject 的用途是**续期**：客户端把手上的令牌带回来，校验通过后沿用
// 它的 subject 再签一个，这样刷新页面、重启浏览器都不会换身份（换身份等于
// 把历史会话留在一个再也拿不回来的 subject 名下）。subject 必须带本 issuer
// 的前缀 —— 否则就变成「拿任意 subject 换一个合法令牌」，那是越权。
func (i *AnonymousIssuer) Issue(subject string) (string, string, time.Time, error) {
	if subject == "" {
		subject = NewAnonymousSubject(i.prefix)
	} else if !strings.HasPrefix(subject, i.prefix) {
		return "", "", time.Time{}, fmt.Errorf(
			"auth: anonymous subject must start with %q", i.prefix,
		)
	}

	expiresAt := i.now().Add(i.ttl)
	payload, err := json.Marshal(tokenPayload{
		Subject:   subject,
		ExpiresAt: expiresAt.Unix(),
	})
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("auth: marshal anonymous payload: %w", err)
	}

	encoded := base64.RawURLEncoding.EncodeToString(payload)
	signed := encoded + "." + base64.RawURLEncoding.EncodeToString(i.sign(encoded))

	return anonymousTokenPrefix + signed, subject, expiresAt, nil
}

// Verify 校验令牌并还原成身份。任何一种失败都只回 false，不区分原因：
// 调用方（token 校验路径）对外的表现一律是 401，区分「过期」与「伪造」只会
// 给探测者提供信息，而客户端能做的动作是一样的（重新申请）。
func (i *AnonymousIssuer) Verify(token string) (Identity, bool) {
	encoded, signature, ok := splitAnonymousToken(token)
	if !ok {
		return Identity{}, false
	}

	expected := i.sign(encoded)
	actual, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return Identity{}, false
	}
	// hmac.Equal 是常量时间比较：逐字节比较会把签名暴露给计时攻击。
	//
	// 注意签名的 base64 表示**不唯一**：32 字节编码成 43 个字符是 258 bit，
	// 多出的 2 bit 会被解码器丢弃，所以「只改了最后一个字符」的令牌解出来的
	// 签名可能与原文完全相同、验签照样通过。这不影响安全性（等价编码不能伪造
	// 签名），但意味着**不能拿令牌串本身当唯一键**去去重或索引。
	if !hmac.Equal(expected, actual) {
		return Identity{}, false
	}

	var payload tokenPayload
	if err := json.Unmarshal(decode(encoded), &payload); err != nil {
		return Identity{}, false
	}
	if payload.Subject == "" || !strings.HasPrefix(payload.Subject, i.prefix) {
		return Identity{}, false
	}
	// 过期判定用 >= ：exp 是「有效期截止的那一刻」，落在那一刻上即失效。
	if i.now().Unix() >= payload.ExpiresAt {
		return Identity{}, false
	}

	return Identity{Subject: payload.Subject, Role: i.role}, true
}

func (i *AnonymousIssuer) sign(encoded string) []byte {
	mac := hmac.New(sha256.New, i.secret)
	mac.Write([]byte(encoded))
	return mac.Sum(nil)
}

// splitAnonymousToken 把 `anon.<payload>.<signature>` 拆成两段。
//
// 前缀在这里判掉，是为了让**静态 API key 走不到这条路径**：静态 key 是一个
// 任意串，命中匿名分支只可能浪费一次 HMAC（甚至误判），先按前缀分流最省。
func splitAnonymousToken(token string) (string, string, bool) {
	rest, ok := strings.CutPrefix(token, anonymousTokenPrefix)
	if !ok {
		return "", "", false
	}
	encoded, signature, ok := strings.Cut(rest, ".")
	if !ok || encoded == "" || signature == "" {
		return "", "", false
	}
	return encoded, signature, true
}

// decode 解 base64url；失败时返回 nil，交给 json.Unmarshal 去拒绝空输入。
func decode(encoded string) []byte {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil
	}
	return raw
}
