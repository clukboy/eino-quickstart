package auth

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// AccountSubjectPrefix 是账号身份的 subject 前缀。
//
// 与匿名前缀（anon:）一样，写成 `<类型>:<id>` 而不是裸 id：库里
// sessions.owner_subject 一眼能看出归属是账号还是访客，登录迁移也靠前缀筛选。
//
// ── 为什么它是常量而不是配置项 ────────────────────────────────────
// 匿名前缀做成可配（见 config.AnonymousAuthConfig.SubjectPrefix）有实际用途：
// 不同部署可以要不同的名字。账号前缀则**必须**在所有部署、所有历史数据里保持
// 一致 —— 它已经写进了 sessions.owner_subject 的存量行，一旦某个部署把它改掉，
// 那个部署里「登录后把匿名历史并过来」就会把行搬到另一个前缀下，而按前缀做的
// 任何查询都再也找不到它们。少一个可以配错的旋钮。
const AccountSubjectPrefix = "user:"

// accountTokenPrefix 是账号令牌的前缀。与匿名令牌（anon.）并列，
// 用于在校验路径上快速分流，避免拿静态 key 去白跑一次 HMAC。
const accountTokenPrefix = "user."

// AccountIssuer 签发并校验账号登录令牌。
//
// ── 与匿名身份唯一的实质差异 ──────────────────────────────────────
// 角色来自**数据库**（users.role）而不是配置，且必须随令牌一起走 —— 服务端
// 不查库，令牌过期前无法感知角色变更。代价是「改角色要等下次登录才生效」，
// 记在 docs/known-gaps.md。同理 must_change_password 也在载荷里（tokenPayload.mcp），
// 所以改密成功后必须换一枚新令牌，光改库是不够的。
//
// ── 无状态带来的第二个代价 ────────────────────────────────────────
// 停用账号（users.status）、改密码，都挡不住**已经签发**的令牌：它到 exp 之前
// 一直有效。唯一的缓解是 ttl 上限（见 config.maxAccountTTLHours）。
// 要做真吊销就得落库存令牌指纹，那是另一套设计（本次不做）。
type AccountIssuer struct {
	subjectIssuer
}

// AccountConfig 是构造 AccountIssuer 的输入。
//
// 刻意**没有 role 字段**：一次误配成 admin 会让所有账号变成管理员，而这个
// 旋钮没有任何正当用途（角色来自 users.role）。
type AccountConfig struct {
	Secret string
	TTL    time.Duration
}

func NewAccountIssuer(cfg AccountConfig) (*AccountIssuer, error) {
	kernel, err := newSubjectIssuer(subjectIssuerConfig{
		Secret: cfg.Secret,
		TTL:    cfg.TTL,
		// 默认角色对账号没有意义：IssueFor 每次都把真实角色写进载荷，
		// identity 会优先取载荷里的那个。给 RoleAgent 只是为了通过内核校验，
		// 顺便保证「万一将来有人签了一枚不带角色的账号令牌」也不会得到 admin。
		Role:          RoleAgent,
		SubjectPrefix: AccountSubjectPrefix,
		TokenPrefix:   accountTokenPrefix,
		Label:         "account",
	})
	if err != nil {
		return nil, err
	}

	return &AccountIssuer{subjectIssuer: kernel}, nil
}

// SubjectPrefix 是账号 subject 的前缀，语义同 AccountSubjectPrefix。
func (i *AccountIssuer) SubjectPrefix() string { return i.subjectPrefix }

// NewAccountSubject 把自增 id 拼成对外身份 `user:<id>`。
func NewAccountSubject(id uint64) string {
	return AccountSubjectPrefix + strconv.FormatUint(id, 10)
}

// ParseAccountSubject 是 NewAccountSubject 的逆运算。
//
// 存在的理由：身份字符串是「谁在请求」在整条链路上的载体（context、会话归属、
// 审批归属），而需要用到数字 id 的地方只有一处（查账号详情）。让那一处自己
// 去 CutPrefix 会把前缀格式复制到第二个包 —— 改格式时必漏一处。
//
// 非账号身份（匿名 subject、静态 key 的 subject）一律返回 false，调用方据此
// 区分「这是一个登录用户」与「这是一个访客」。
func ParseAccountSubject(subject string) (uint64, bool) {
	raw, ok := strings.CutPrefix(subject, AccountSubjectPrefix)
	if !ok {
		return 0, false
	}

	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, false
	}

	return id, true
}

// IssueFor 为某个账号签发令牌。
//
// subject 必须是 `user:<id>`（由调用方用 NewAccountSubject 拼好），不接受空值：
// 账号不像匿名身份那样可以「没有 id 就先发一个」，它必须有真实的数据库主键。
//
// role 在这里被显式校验而不是交给内核：内核的 role 是**默认值**（可以为空），
// 而账号令牌的角色是要写进载荷的真实角色，写一个内核不认识的枚举值进去，
// 会在 verify 时被 identity 拒绝 —— 那是一个已经签出去、谁也验不过的令牌，
// 比在签发时就报错糟得多。
func (i *AccountIssuer) IssueFor(
	subject string,
	role Role,
	mustChangePassword bool,
) (string, time.Time, error) {
	// 用 ParseAccountSubject 而不是只比前缀：`user:`（前缀后面什么都没有）、
	// `user:abc`（不是数字）都能通过前缀检查，却会签出一枚 subject 无法解析的
	// 令牌 —— 那枚令牌能验签通过，但拿到它之后按 id 查账号必然失败，症状是
	// 「已登录却查不到自己」。要求它必须能被解析回去，就把这个缺口堵在签发时。
	if _, ok := ParseAccountSubject(subject); !ok {
		return "", time.Time{}, fmt.Errorf(
			"auth: account subject must be %q followed by a numeric id, got %q",
			AccountSubjectPrefix,
			subject,
		)
	}

	switch role {
	case RoleAgent, RoleApprover, RoleAdmin:
	default:
		return "", time.Time{}, fmt.Errorf(
			"auth: account role %q is not a known role", role,
		)
	}

	token, expiresAt, err := i.issue(subject, role, mustChangePassword)
	if err != nil {
		return "", time.Time{}, err
	}

	return token, expiresAt, nil
}

// Verify 校验账号令牌。失败一律回 false（401），不区分「过期 / 伪造 / 角色
// 不认识」—— 对客户端来说三种情况的唯一动作都是重新登录。
func (i *AccountIssuer) Verify(token string) (Identity, bool) {
	payload, ok := i.verify(token)
	if !ok {
		return Identity{}, false
	}

	return i.identity(payload)
}

var _ interface {
	SubjectPrefix() string
	IssueFor(subject string, role Role, mustChangePassword bool) (string, time.Time, error)
	Verify(token string) (Identity, bool)
} = (*AccountIssuer)(nil)
