package auth

import (
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
// ── 与账号身份的关系 ───────────────────────────────────────────────
// 签名内核在 issuer.go 的 subjectIssuer 里，本类型只是给它填上「密钥、anon
// 前缀、roles 默认 agent」这一组参数。账号身份（account.go）填的是另一组，
// 两者的差异仅此而已 —— 校验路径与业务侧（actorSubject、会话归属、角色
// 中间件）一行都不用改，因为它们拿到的都是一个 Identity。
type AnonymousIssuer struct {
	subjectIssuer
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
	prefix := cfg.SubjectPrefix
	if prefix == "" {
		prefix = AnonymousSubjectPrefix
	}

	kernel, err := newSubjectIssuer(subjectIssuerConfig{
		Secret:        cfg.Secret,
		TTL:           cfg.TTL,
		Role:          cfg.Role,
		SubjectPrefix: prefix,
		TokenPrefix:   anonymousTokenPrefix,
		Label:         "anonymous",
	})
	if err != nil {
		return nil, err
	}

	return &AnonymousIssuer{subjectIssuer: kernel}, nil
}

// Role 是匿名身份被授予的角色，供签发接口回传给客户端（前端据此说明
// 「当前是访客，只能访问对话相关接口」）。
func (i *AnonymousIssuer) Role() Role { return i.role }

// SubjectPrefix 是匿名 subject 的前缀，语义同 AnonymousSubjectPrefix。
func (i *AnonymousIssuer) SubjectPrefix() string { return i.subjectPrefix }

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
//
// 刻意不把角色写进载荷：匿名角色来自配置，每个请求都一样，让它跟着每个令牌
// 走只会给「客户端影响角色」留下想象空间。载荷里没有 r 时，verify 会回落到
// issuer 的 role（见 subjectIssuer.identity）。
func (i *AnonymousIssuer) Issue(subject string) (string, string, time.Time, error) {
	if subject == "" {
		subject = NewAnonymousSubject(i.subjectPrefix)
	} else if err := i.checkSubject(subject); err != nil {
		return "", "", time.Time{}, err
	}

	token, expiresAt, err := i.issue(subject, "", false)
	if err != nil {
		return "", "", time.Time{}, err
	}

	return token, subject, expiresAt, nil
}

// Verify 校验令牌并还原成身份。任何一种失败都只回 false，不区分原因：
// 调用方（token 校验路径）对外的表现一律是 401，区分「过期」与「伪造」只会
// 给探测者提供信息，而客户端能做的动作是一样的（重新申请）。
func (i *AnonymousIssuer) Verify(token string) (Identity, bool) {
	payload, ok := i.verify(token)
	if !ok {
		return Identity{}, false
	}

	return i.identity(payload)
}

// 编译期断言：AnonymousIssuer 必须仍然满足「能签发、能校验、能报角色」这个
// 对外形状。它现在通过内嵌 subjectIssuer 拿到大部分能力，接口变了这里会先炸。
var _ interface {
	Role() Role
	SubjectPrefix() string
	Issue(subject string) (string, string, time.Time, error)
	Verify(token string) (Identity, bool)
} = (*AnonymousIssuer)(nil)
