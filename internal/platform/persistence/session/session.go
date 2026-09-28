package session

import (
	"context"
	"eino-quickstart/ent"
	entSession "eino-quickstart/ent/session"
	"eino-quickstart/ent/sessionmessage"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"
	_ "github.com/lib/pq"
)

var ErrNotFoundOrForbidden = errors.New(
	"session not found or access denied",
)

// TitleLimit 是会话标题的**字符**上限（不是字节数 —— 中文标题按字算，
// 按字节截会把一个汉字切成两半）。
//
// ⚠️ 前端 src/api/model/chat.ts 里有一个同名的 sessionTitleFrom，两边的规则
// 必须逐字一致（trim → 把连续空白折叠成单个空格 → 超限截断加 …）。规则不一致
// 的后果很具体：刚发完消息时侧栏显示的是前端派生的标题，刷新后换成服务端存的
// 那个，同一句话会变成两个标题。
// 存量的库侧回填见 internal/platform/persistence/session 的历史迁移说明。
const TitleLimit = 18

// SessionRecord 是会话的只读视图，不带消息 —— 会话列表只关心这几个字段。
type SessionRecord struct {
	// ID 是会话的对外 id，也就是 sessions.session_id（uuid）。
	ID        string
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// MessageRecord 是一条历史消息。
type MessageRecord struct {
	ID uint64
	// Role 只有 "user" / "assistant" 两种，取值与 eino schema.RoleType 一致
	// （写入侧就是 string(schema.User) / string(schema.Assistant)）。
	Role      string
	Content   string
	CreatedAt time.Time
}

type Store struct {
	client *ent.Client
}

func NewStore(client *ent.Client) *Store {
	return &Store{client: client}
}

// DeriveTitle 从一条用户消息派生会话标题。
//
// 空消息（或只有空白）返回空串，调用方据此决定「不写」而不是「写个空标题」——
// 用空串表示「这条会话还没有人说过话」是 sessions.title 的约定。
func DeriveTitle(text string) string {
	// strings.Fields 按 Unicode 空白切分并丢掉空段，等价于
	// 「trim + 把连续空白折叠成单个空格」。
	flat := strings.Join(strings.Fields(text), " ")
	if flat == "" {
		return ""
	}

	if utf8.RuneCountInString(flat) <= TitleLimit {
		return flat
	}

	return string([]rune(flat)[:TitleLimit]) + "…"
}

// SetTitleIfEmpty 只在会话标题还是空的时候写入。
//
// 「只在空时写」是刻意的：标题的语义是**首条**用户消息，后面每一条消息都不该
// 改写它。把「读-判断-写」合成一条 UPDATE ... WHERE title = ” 而不是先查后写，
// 是为了让并发的两条首消息只有一个能赢，另一个静默跳过 —— 先查后写会出现
// 两条消息都读到空、都去写、后写的覆盖先写的。
func (s *Store) SetTitleIfEmpty(
	ctx context.Context,
	sessionID string,
	subject string,
	title string,
) error {
	if title == "" {
		return nil
	}

	// 不检查 affected。它等于 0 有两种可能，都无法从这一条语句区分，
	// 且都不是错误：
	//   - 标题已经有人写过了（正常：这条不是首条消息）
	//   - 会话不存在或不属于 subject（越权）—— 调用方（Chat）不该因此失败
	_, err := s.client.Session.
		Update().
		Where(
			entSession.SessionIDEQ(sessionID),
			entSession.OwnerSubjectEQ(subject),
			entSession.TitleEQ(""),
		).
		SetTitle(title).
		Save(ctx)

	return err
}

// List 按最后活动时间倒序列出某个主体的会话。
//
// 不过滤空会话：用户刚点的「新对话」就应该出现在列表里，否则他会以为没建成功。
// 空会话产生的唯一来源是「点新建但没说话」，那是用户自己的动作，不做隐藏。
func (s *Store) List(ctx context.Context, subject string) ([]SessionRecord, error) {
	records, err := s.client.Session.
		Query().
		Where(entSession.OwnerSubjectEQ(subject)).
		// ID 是兜底：updated_at 精度到微秒，仍可能撞（同一事务里连着建两条），
		// 撞了就靠自增主键定序，免得每次刷新的顺序都在抖。
		Order(
			ent.Desc(entSession.FieldUpdatedAt),
			ent.Desc(entSession.FieldID),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}

	sessions := make([]SessionRecord, 0, len(records))
	for _, record := range records {
		sessions = append(sessions, SessionRecord{
			ID:        record.SessionID,
			Title:     record.Title,
			CreatedAt: record.CreatedAt,
			UpdatedAt: record.UpdatedAt,
		})
	}

	return sessions, nil
}

// Messages 按时间正序返回一个会话的全部历史消息（user 与 assistant 混在一起）。
// 会话不存在、或不属于 subject 时统一返回 ErrNotFoundOrForbidden。
func (s *Store) Messages(ctx context.Context, sessionID string, subject string) ([]MessageRecord, error) {
	// 先确认会话归属，不能只查消息表。
	//
	// 两个原因：消息表本身不带 owner，「查不到消息」既可能是没权限、也可能是
	// 会话刚建好还没说话 —— 前者必须 404、后者必须返回空列表，只看消息条数
	// 区分不出来；而把两者合并成「返回空」会让越权请求看起来像正常请求。
	if _, err := s.getOwned(ctx, sessionID, subject); err != nil {
		return nil, err
	}

	records, err := s.client.SessionMessage.
		Query().
		Where(sessionmessage.HasSessionWith(
			entSession.SessionIDEQ(sessionID),
		)).
		Order(
			ent.Asc(sessionmessage.FieldCreatedAt),
			ent.Asc(sessionmessage.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	messages := make([]MessageRecord, 0, len(records))
	for _, record := range records {
		messages = append(messages, MessageRecord{
			ID:        record.ID,
			Role:      record.Role,
			Content:   record.Content,
			CreatedAt: record.CreatedAt,
		})
	}

	return messages, nil
}

func (s *Store) GetOrCreate(ctx context.Context, id string, subject string) error {
	_, err := s.getOrCreate(ctx, id, subject)
	return err
}

// History 是 Messages 的 eino 视图：对话链路要的是喂给模型的消息序列。
//
// 与 Messages 的区别只有返回类型 —— 查询、排序、越权口径全部共用，
// 免得两条读路径各自演化出不一致的行为。
func (s *Store) History(
	ctx context.Context,
	sessionID string,
	subject string,
) ([]*schema.Message, error) {
	records, err := s.Messages(ctx, sessionID, subject)
	if err != nil {
		return nil, err
	}

	messages := make([]*schema.Message, len(records))
	for i, record := range records {
		messages[i] = &schema.Message{
			Role:    schema.RoleType(record.Role),
			Content: record.Content,
		}
	}

	return messages, nil
}

func (s *Store) Append(ctx context.Context, sessionID string, subject string, message *schema.Message) error {
	sessionRecord, err := s.getOwned(ctx, sessionID, subject)
	if err != nil {
		return err
	}

	return s.client.SessionMessage.
		Create().
		SetSession(sessionRecord).
		SetRole(string(message.Role)).
		SetContent(message.Content).
		Exec(ctx)
}
func (s *Store) getOrCreate(ctx context.Context, id string, subject string) (*ent.Session, error) {
	record, err := s.getOwned(ctx, id, subject)
	if err == nil {
		if record.OwnerSubject != subject {
			return nil, ErrNotFoundOrForbidden
		}
		return record, nil
	}
	if !errors.Is(err, ErrNotFoundOrForbidden) {
		return nil, err
	}

	record, err = s.client.Session.
		Create().
		SetSessionID(id).
		SetOwnerSubject(subject).
		Save(ctx)
	if err == nil {
		return record, nil
	}
	if !ent.IsConstraintError(err) {
		return nil, err
	}

	return s.getOwned(ctx, id, subject)
}

func (s *Store) getOwned(
	ctx context.Context,
	sessionID string,
	subject string,
) (*ent.Session, error) {
	record, err := s.client.Session.
		Query().
		Where(entSession.SessionIDEQ(sessionID)).
		Only(ctx)

	if ent.IsNotFound(err) {
		return nil, ErrNotFoundOrForbidden
	}
	if err != nil {
		return nil, err
	}

	if record.OwnerSubject != subject {
		// 统一返回 NotFound，避免通过 UUID 猜测资源是否存在。
		return nil, ErrNotFoundOrForbidden
	}

	return record, nil
}

func (s *Store) Ready(ctx context.Context) error {
	_, err := s.client.Session.Query().Limit(1).Count(ctx)
	return err
}
