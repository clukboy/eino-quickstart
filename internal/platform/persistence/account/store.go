package account

import (
	"context"
	"errors"
	"time"

	"eino-quickstart/ent"
	entUser "eino-quickstart/ent/user"
)

// ErrUsernameTaken 表示建号时用户名已被占用（users.username 上的唯一索引）。
var ErrUsernameTaken = errors.New("account: username is already taken")

// ErrNotFound 表示账号不存在。
//
// 登录与「查询当前账号」都用它，但**不要**把它直接透给客户端：登录路径上
// 「没有这个用户」与「密码不对」必须回同一个 401，否则接口就成了一个用户名
// 枚举器（见 logic/auth/login_logic.go）。
var ErrNotFound = errors.New("account: account not found")

// Record 是账号的只读视图。
//
// 它刻意**不含 password_hash**：这样「把账号返回给 HTTP 层」这件事在类型上
// 就不可能顺带把哈希带出去，而不依赖每个 DTO 手写一遍「记得别加这个字段」。
// 需要哈希的调用方只有一个（登录校验），它走 ByUsername 拿 Credentials。
type Record struct {
	// ID 是自增主键。对外身份字符串是 `user:<ID>`（见 auth.NewAccountSubject），
	// 拼接与解析都归 auth 包管，本包只认数字 id。
	ID       uint64
	Username string
	// Role 取值与 ent/user 的枚举一致：agent / approver / admin。
	Role string
	// Status 取值 ACTIVE / DISABLED。
	Status             string
	MustChangePassword bool
	CreatedAt          time.Time
	// PasswordChangedAt 为空表示从未改过密码。
	PasswordChangedAt *time.Time
}

// Active 报告账号是否处于可登录状态。
//
// 判断放在这里而不是让 HTTP 层去比字符串：status 的取值来自 ent 的枚举，谁改
// 了 schema 谁就该改这里，而不是让全仓库去 grep "ACTIVE"。注意它只在**登录时**
// 被检查 —— 账号令牌是无状态的，停用一个账号不会让已签发的令牌失效。
func (r Record) Active() bool { return r.Status == string(entUser.StatusACTIVE) }

// Credentials 是 Record 加上密码哈希，只给登录校验用。
type Credentials struct {
	Record
	PasswordHash string
}

type Store struct {
	client *ent.Client
}

func NewStore(client *ent.Client) *Store {
	return &Store{client: client}
}

// Create 建一个账号。
//
// ── 为什么签名里没有 role ──────────────────────────────────────────
// 调用方拿不到「把角色设成 admin」的能力，哪怕它想传。建号接口因此天然不可能
// 被越权提权（决策 5），而这个保证是写在**类型**里的，不是写在某个 handler 的
// 参数校验里的 —— 后者会在下一次重构时被顺手删掉。
//
// 同理 mustChangePassword 也不可传：管理员设定的初始密码只能用一次，这条规则
// 不该取决于调用方记不记得传 true。
//
// passwordHash 必须是**已经哈希过**的（bcrypt）。本包不做哈希，因为哈希成本
// 是纯 CPU 且要在一次请求里只做一次，放在 logic 层更容易被看见和调优。
func (s *Store) Create(ctx context.Context, username string, passwordHash string) (*Record, error) {
	if username == "" || passwordHash == "" {
		return nil, errors.New("account: username and password hash are required")
	}

	record, err := s.client.User.
		Create().
		SetUsername(username).
		SetPasswordHash(passwordHash).
		// 三项都显式写出来而不是靠 schema 默认值：默认值是「建表时」的约定，
		// 这里显式声明的是「建号时」的业务规则，两者将来可能分别演化。
		SetRole(entUser.RoleAgent).
		SetStatus(entUser.StatusACTIVE).
		SetMustChangePassword(true).
		Save(ctx)
	if ent.IsConstraintError(err) {
		return nil, ErrUsernameTaken
	}
	if err != nil {
		return nil, err
	}

	return fromEnt(record), nil
}

// List 按创建时间倒序列出全部账号（新建的排在最前）。
//
// 本轮不分页：与 /sessions 同一取舍，先跑通再按需加 —— 账号数量在人工建号的
// 前提下天然很小。真到了要分页的时候，请把 page/pageSize 加成显式参数，而不是
// 在这里默默加一个 limit。
func (s *Store) List(ctx context.Context) ([]Record, error) {
	records, err := s.client.User.
		Query().
		// ID 是兜底：created_at 精度到微秒仍可能撞（同一请求里连着建两条），
		// 撞了就靠自增主键定序，免得每次刷新的顺序都在抖。
		Order(
			ent.Desc(entUser.FieldCreatedAt),
			ent.Desc(entUser.FieldID),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}

	list := make([]Record, 0, len(records))
	for _, record := range records {
		list = append(list, *fromEnt(record))
	}

	return list, nil
}

// ByUsername 拿登录校验需要的那一份（含哈希）。
//
// 查不到返回 ErrNotFound —— 调用方**必须**把它和「密码不对」合并成同一个对外
// 响应，并且仍然跑一次假 bcrypt 抹平时序（见 logic/auth/login_logic.go）。
func (s *Store) ByUsername(ctx context.Context, username string) (*Credentials, error) {
	record, err := s.client.User.
		Query().
		Where(entUser.UsernameEQ(username)).
		Only(ctx)

	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	return credentialsFrom(record), nil
}

// CredentialsByID 按自增主键取登录校验需要的那一份（含哈希）。
//
// 与 ByID 分开，是为了守住「Record 不含哈希」这条线。唯一需要按 id 拿哈希的
// 场景是改密前校验当前密码；给它一个名字里就写着 Credentials 的方法，比让
// ByID 一直带着哈希、再靠调用方自觉不去用要可靠 —— 后者会在某次重构里
// 悄悄失效，而失效的表现是哈希进了某个 DTO。
func (s *Store) CredentialsByID(ctx context.Context, id uint64) (*Credentials, error) {
	record, err := s.client.User.
		Query().
		Where(entUser.IDEQ(id)).
		Only(ctx)

	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	return credentialsFrom(record), nil
}

// ByID 按自增主键取账号。
//
// 这里**没有** BySubject：对外身份是 `user:<id>`，而前缀的归属在 auth 包。
// 让持久层去解析一个由上层定义的字符串格式，等于把「前缀长什么样」这个知识
// 复制到两个包，早晚漂。调用方应该先 auth.ParseAccountSubject 拿到 id 再来。
func (s *Store) ByID(ctx context.Context, id uint64) (*Record, error) {
	record, err := s.client.User.
		Query().
		Where(entUser.IDEQ(id)).
		Only(ctx)

	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	return fromEnt(record), nil
}

// ChangePassword 写入新哈希并**同时**清掉 must_change_password。
//
// 两件事必须一起做，否则会出现「密码已经改了、但每次登录仍然被强制改密」的
// 死循环。调用方（logic/auth/password_logic.go）在成功后还要签发一枚新令牌
// —— 因为 mcp 也签在令牌里，光改库不改令牌的话，手上的旧令牌仍会被中间件拦。
func (s *Store) ChangePassword(ctx context.Context, id uint64, passwordHash string) error {
	if passwordHash == "" {
		return errors.New("account: password hash is required")
	}

	affected, err := s.client.User.
		Update().
		Where(entUser.IDEQ(id)).
		SetPasswordHash(passwordHash).
		SetMustChangePassword(false).
		SetPasswordChangedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}

	return nil
}

// MigrateAnonymousHistory 把 from 名下的会话整体改挂到 to 名下，返回迁移条数。
//
// ── 为什么是裸 SQL ────────────────────────────────────────────────
// ent/schema/session.go 里 owner_subject 标了 Immutable()，ent 因此**根本没有
// 生成** Update().SetOwnerSubject(...)，编译器会直接拒绝。这不是疏漏：归属
// 本来就该在创建时定死，能被改的话「这条会话是谁的」就不再可信。而登录迁移是
// 唯一一个合法的改写场景，所以它单独走一条显式的 SQL，让「这里确实在改写归属」
// 在 review 时躲不掉。
//
// ── 为什么只动 sessions ──────────────────────────────────────────
// chat_turns.requested_by、agent_runs.requested_by、approvals.requested_by 也是
// 不可变的，但**刻意不动**：它们记录的是「当时是谁发起的这次执行」，把历史
// 改写成登录后的身份等于伪造审计。真正影响用户可见行为的只有会话列表，而它由
// sessions.owner_subject 驱动。
//
// 单条语句本身就是原子的，所以不包事务 —— 外面套一层 WithTx 只会多一次
// BEGIN/COMMIT 和一条日志，换不来任何一致性保证。
func (s *Store) MigrateAnonymousHistory(
	ctx context.Context,
	from string,
	to string,
) (int64, error) {
	if from == "" || to == "" {
		return 0, errors.New("account: migration subjects are required")
	}
	if from == to {
		// 幂等：同一个 subject 之间搬等于什么都没做。调用方可能因为「用户重复
		// 登录」而重放这一步，直接短路比让库跑一条恒 0 行的 UPDATE 更清楚。
		return 0, nil
	}

	result, err := s.client.ExecContext(
		ctx,
		`UPDATE sessions SET owner_subject = $1 WHERE owner_subject = $2`,
		to,
		from,
	)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

func fromEnt(record *ent.User) *Record {
	return &Record{
		ID:                 record.ID,
		Username:           record.Username,
		Role:               string(record.Role),
		Status:             string(record.Status),
		MustChangePassword: record.MustChangePassword,
		CreatedAt:          record.CreatedAt,
		PasswordChangedAt:  record.PasswordChangedAt,
	}
}

func credentialsFrom(record *ent.User) *Credentials {
	return &Credentials{
		Record:       *fromEnt(record),
		PasswordHash: record.PasswordHash,
	}
}
