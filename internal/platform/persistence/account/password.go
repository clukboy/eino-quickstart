package account

import (
	"fmt"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// 明文密码的长度约束，按**字节**算。
const (
	// MinPasswordLength 是下限。8 位不是密码学上的强要求，而是「挡掉
	// 123456 / admin / 生日」这一档的最低门槛 —— 本系统没有登录限流
	// （见 docs/known-gaps.md），真正的防线是 bcrypt 的成本与有效期。
	MinPasswordLength = 8

	// MaxPasswordLength 是 bcrypt 的硬限制，不是我们挑的数字。
	//
	// bcrypt 只使用前 72 个字节，更长的部分被**静默忽略**。不拒绝的话会有两个
	// 后果：用户以为自己设了个 100 字符的强密码，实际强度只到第 72 字节；
	// 更糟的是他把第 73 个字符改掉时「密码没变」，改密看起来成功了却毫无作用。
	MaxPasswordLength = 72
)

// ValidatePassword 校验明文密码是否符合本系统的长度要求。
//
// 返回的错误消息是**给最终用户看的**（中文、可以直接展示），由传输层包成
// 400 原样返回。之所以不定义 sentinel error 让调用方各自映射文案：调用点只有
// 两个（建号与改密），而两处的文案本来就该一致 —— 分开映射迟早出现
// 「建号说至少 8 位、改密说至少 6 位」这种自相矛盾。
//
// 它只做长度检查，不做强度检查（大小写/数字/符号）：那类规则会把用户逼向
// "Passw0rd!" 这种可预测的变形，收益不如一个足够长的短语。
func ValidatePassword(password string) error {
	if len(password) < MinPasswordLength {
		return fmt.Errorf("密码至少 %d 位", MinPasswordLength)
	}
	if len(password) > MaxPasswordLength {
		return fmt.Errorf(
			"密码最多 %d 个字节（更长的部分会被 bcrypt 忽略）", MaxPasswordLength,
		)
	}

	return nil
}

// NormalizeCost 把配置里的 bcrypt 成本归一成可直接使用的值。
//
// 0 表示「没配」→ bcrypt.DefaultCost。越界值也走默认值而不是报错：配置校验
// （internal/platform/config）已经在启动时拦过一遍，走到这里的越界值只可能来自
// 程序内部，此时降级成默认值比签出一个「谁也验不过」的哈希要好。
func NormalizeCost(cost int) int {
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		return bcrypt.DefaultCost
	}

	return cost
}

// HashPassword 用给定的成本哈希明文密码（cost 为 0 表示默认值）。
//
// 哈希只在这里做，不在 HTTP 逻辑层：建号与改密两处都要用，分开写迟早出现
// 「一处用默认成本、另一处用配置成本」的不一致，而那意味着同一次部署里两个
// 入口的抵抗离线爆破能力不同。
func HashPassword(password string, cost int) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), NormalizeCost(cost))
	if err != nil {
		return "", err
	}

	return string(hash), nil
}

// CheckPassword 比对明文与哈希。
//
// 包一层而不是让调用方直接用 bcrypt 的理由：调用方要的是「对/不对」这个布尔，
// 而 bcrypt.CompareHashAndPassword 返回的 ErrMismatchedHashAndPassword 常常被
// 误当成「服务器错误」写进 500 分支 —— 那会把「密码打错了」变成告警噪声，也会
// 让改密接口把「当前密码不对」回成 500 而不是 400。
func CheckPassword(passwordHash string, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)) == nil
}

var (
	// dummyHashMu 保护下面这对缓存。用互斥锁而不是 sync.Once：成本是配置项，
	// 虽然运行期不变，但缓存与实际成本必须成对，否则会出现「配置改了、假哈希
	// 还是旧成本」的静默错配。
	dummyHashMu   sync.Mutex
	dummyHashCost int
	dummyHash     []byte
)

// BurnPasswordCheck 在没有可比对的目标时，仍然烧掉一次真实成本的 bcrypt。
//
// ── 为什么必须做 ──────────────────────────────────────────────────
// 不存在的用户名会跳过密码比对，耗时几微秒；存在的用户名要多花一次 bcrypt
// （默认成本约几十毫秒）。这个差距有三个数量级，攻击者不需要看响应内容，只看
// 响应时间就能枚举出哪些用户名存在 —— 即使两种情况的错误码与文案完全一致。
//
// 用的是**按当前成本现算的**哈希，而不是硬编码常量：bcrypt 的耗时只取决于
// 哈希里记录的成本，硬编码一个成本 10 的哈希在配置成本 14 的部署上抹不平。
func BurnPasswordCheck(cost int) {
	_ = bcrypt.CompareHashAndPassword(dummyHashFor(cost), []byte("wrong-password"))
}

func dummyHashFor(cost int) []byte {
	cost = NormalizeCost(cost)

	dummyHashMu.Lock()
	defer dummyHashMu.Unlock()

	if dummyHash != nil && dummyHashCost == cost {
		return dummyHash
	}

	hash, err := bcrypt.GenerateFromPassword(
		[]byte("timing-equalisation-placeholder"),
		cost,
	)
	if err != nil {
		// 留空表示「这次没法抹平」，而不是 panic：成本越界已经由 NormalizeCost
		// 归一过，这里失败只可能是 bcrypt 自身出错（例如密码里含 NUL ——
		// 这个常量不含）。
		return nil
	}

	dummyHash, dummyHashCost = hash, cost

	return dummyHash
}
