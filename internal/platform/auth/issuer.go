package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// tokenPayload 是令牌里自包含的载荷。
//
// 字段名取短名（s / exp / r / mcp）：这个串会出现在每个请求的 Authorization
// 头里，它只对签发方与校验方有意义，可读性由本文件的注释负责。
type tokenPayload struct {
	Subject   string `json:"s"`
	ExpiresAt int64  `json:"exp"`

	// Role 与 MustChangePassword 是**账号令牌**才有的两个字段。
	//
	// 匿名令牌不写它们（omitempty 让载荷与加字段之前逐字节相同），于是
	// 「载荷里没有 r」就自然成为「用 issuer 构造期写死的角色」的表示 ——
	// 这正是匿名令牌需要的语义（匿名角色来自配置，不该让客户端有机会影响），
	// 同时让加字段之前签出去的旧令牌继续可用。
	//
	// 账号令牌必须写 r：一个账号的角色存在 users.role 里，会随改库变化，而
	// 令牌是无状态的（服务端不查库），角色只能随令牌一起走。代价见
	// docs/known-gaps.md：改了角色要等下次登录才生效。
	Role string `json:"r,omitempty"`

	// MustChangePassword 是「管理员设定的初始密码还没换过」。
	//
	// 它必须**签进令牌**而不是每次查库，因为拦截动作发生在全局中间件里
	// （middleware/password_guard_middleware.go），那里不该有数据库依赖；
	// 而它又必须能挡住已签发的令牌，所以不能在登录时算一次就丢掉。
	MustChangePassword bool `json:"mcp,omitempty"`
}

// subjectIssuer 是「subject + 过期时间 + 角色」这一族令牌的签发内核。
//
// ── 为什么要抽出来 ────────────────────────────────────────────────
// 匿名身份与账号身份的区别只有三件事：密钥、subject 前缀、令牌前缀。签名、
// 编码、验签、过期判定逐字相同。复制成两份的后果不是「多几十行」，而是
// 「过期判定用 >= 还是 >」「签名比较用 == 还是 hmac.Equal」这类改动**只会改到
// 其中一处** —— 留下一枚验签强度不同的旁路令牌通道，而两边的单测都是绿的。
//
// ── 为什么内嵌而不是接口 ──────────────────────────────────────────
// 两个 issuer 的对外方法（Issue / Verify / Role / SubjectPrefix）语义并不相同：
// 匿名允许 subject 为空（新身份）、账号不允许；账号的 Issue 还要带角色和改密
// 标记。让它们共用一个接口只会把差异挤进一个越来越长的参数表里。内核只暴露
// **共同的那部分原语**，各自的方法在自家文件里表达差异。
type subjectIssuer struct {
	secret []byte
	ttl    time.Duration

	// role 是**默认角色**：载荷里没有 r 时用它。匿名令牌永远走这条路径。
	role Role

	subjectPrefix string
	tokenPrefix   string

	// label 只用于错误文案，让启动日志能指出是哪个 issuer 配置错了
	// （两个 issuer 的 secret 是分别配置的，报错不说清楚就没法排查）。
	label string

	// now 可注入，只为让「过期」这条分支在测试里能确定性地触发。
	now func() time.Time
}

type subjectIssuerConfig struct {
	Secret        string
	TTL           time.Duration
	Role          Role
	SubjectPrefix string
	TokenPrefix   string
	Label         string
}

// newSubjectIssuer 校验并构造内核。
//
// 所有校验都在这里，所以两个 issuer 不可能「一个校验了密钥一个没校验」。
// 空 secret 一律拒绝而不是降级：空密钥等于任何人都能伪造任意 subject 的令牌。
func newSubjectIssuer(cfg subjectIssuerConfig) (subjectIssuer, error) {
	if cfg.Label == "" {
		return subjectIssuer{}, fmt.Errorf("auth: subject issuer label is required")
	}
	if cfg.Secret == "" {
		return subjectIssuer{}, fmt.Errorf("auth: %s secret is required", cfg.Label)
	}
	if cfg.TTL <= 0 {
		return subjectIssuer{}, fmt.Errorf(
			"auth: %s token ttl must be greater than zero", cfg.Label,
		)
	}
	if cfg.SubjectPrefix == "" || cfg.TokenPrefix == "" {
		return subjectIssuer{}, fmt.Errorf(
			"auth: %s subject prefix and token prefix are required", cfg.Label,
		)
	}

	role := cfg.Role
	if role == "" {
		role = RoleAgent
	}
	switch role {
	case RoleAgent, RoleApprover, RoleAdmin:
	default:
		return subjectIssuer{}, fmt.Errorf(
			"auth: %s role %q is not a known role", cfg.Label, role,
		)
	}

	return subjectIssuer{
		secret:        []byte(cfg.Secret),
		ttl:           cfg.TTL,
		role:          role,
		subjectPrefix: cfg.SubjectPrefix,
		tokenPrefix:   cfg.TokenPrefix,
		label:         cfg.Label,
		now:           time.Now,
	}, nil
}

// issue 把 subject / 角色 / 改密标记签成一枚令牌。
//
// 不做任何 subject 语义校验（前缀、是否允许为空）—— 那是各家 Issue 的事，
// 因为它们对「空 subject」的态度正好相反。这里只管签发这一件事。
func (i *subjectIssuer) issue(
	subject string,
	role Role,
	mustChangePassword bool,
) (string, time.Time, error) {
	expiresAt := i.now().Add(i.ttl)

	payload, err := json.Marshal(tokenPayload{
		Subject:            subject,
		ExpiresAt:          expiresAt.Unix(),
		Role:               string(role),
		MustChangePassword: mustChangePassword,
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: marshal %s payload: %w", i.label, err)
	}

	encoded := base64.RawURLEncoding.EncodeToString(payload)
	signed := encoded + "." + base64.RawURLEncoding.EncodeToString(i.sign(encoded))

	return i.tokenPrefix + signed, expiresAt, nil
}

// checkSubject 拒绝不属于本 issuer 的 subject。
//
// 这是**越权防线**而不是格式检查：能续期别人的 subject，就等于能拿一个合法
// 令牌去读写别人的会话。匿名接口与账号接口都建立在它之上。
func (i *subjectIssuer) checkSubject(subject string) error {
	if !strings.HasPrefix(subject, i.subjectPrefix) {
		return fmt.Errorf(
			"auth: %s subject must start with %q", i.label, i.subjectPrefix,
		)
	}

	return nil
}

// verify 校验令牌并还原出载荷。
//
// 任何一种失败都只回 false，不区分原因：调用方（token 校验路径）对外的表现
// 一律是 401，区分「过期」与「伪造」只会给探测者提供信息，而客户端能做的动作
// 是一样的（重新申请或重新登录）。
func (i *subjectIssuer) verify(token string) (tokenPayload, bool) {
	encoded, signature, ok := i.split(token)
	if !ok {
		return tokenPayload{}, false
	}

	expected := i.sign(encoded)
	actual, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return tokenPayload{}, false
	}
	// hmac.Equal 是常量时间比较：逐字节比较会把签名暴露给计时攻击。
	//
	// 注意签名的 base64 表示**不唯一**：32 字节编码成 43 个字符是 258 bit，
	// 多出的 2 bit 会被解码器丢弃，所以「只改了最后一个字符」的令牌解出来的
	// 签名可能与原文完全相同、验签照样通过。这不影响安全性（等价编码不能伪造
	// 签名），但意味着**不能拿令牌串本身当唯一键**去去重或索引。
	if !hmac.Equal(expected, actual) {
		return tokenPayload{}, false
	}

	var payload tokenPayload
	if err := json.Unmarshal(decode(encoded), &payload); err != nil {
		return tokenPayload{}, false
	}
	if payload.Subject == "" || !strings.HasPrefix(payload.Subject, i.subjectPrefix) {
		return tokenPayload{}, false
	}
	// 过期判定用 >= ：exp 是「有效期截止的那一刻」，落在那一刻上即失效。
	if i.now().Unix() >= payload.ExpiresAt {
		return tokenPayload{}, false
	}

	return payload, true
}

// identity 把载荷还原成身份：载荷里的角色优先，没有则用 issuer 的默认角色。
func (i *subjectIssuer) identity(payload tokenPayload) (Identity, bool) {
	role := i.role
	if payload.Role != "" {
		role = Role(payload.Role)
		// 载荷虽被签名保护，角色取值仍要复核：密钥轮换、或同一个密钥被两类
		// issuer 共用时，签名有效并不保证角色是本系统认识的枚举值。
		switch role {
		case RoleAgent, RoleApprover, RoleAdmin:
		default:
			return Identity{}, false
		}
	}

	return Identity{
		Subject:            payload.Subject,
		Role:               role,
		MustChangePassword: payload.MustChangePassword,
	}, true
}

func (i *subjectIssuer) sign(encoded string) []byte {
	mac := hmac.New(sha256.New, i.secret)
	mac.Write([]byte(encoded))
	return mac.Sum(nil)
}

// split 把 `<tokenPrefix><payload>.<signature>` 拆成两段。
//
// 前缀在这里判掉，是为了让**静态 API key 走不到这条路径**：静态 key 是一个
// 任意串，命中验签分支只可能浪费一次 HMAC（甚至误判），先按前缀分流最省。
func (i *subjectIssuer) split(token string) (string, string, bool) {
	rest, ok := strings.CutPrefix(token, i.tokenPrefix)
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
