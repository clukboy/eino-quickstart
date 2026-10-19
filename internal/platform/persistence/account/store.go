package account

import (
	"context"
	"errors"
	"time"

	"eino-quickstart/ent"
	entUser "eino-quickstart/ent/user"
)

var ErrUsernameTaken = errors.New("account: username is already taken")

var ErrNotFound = errors.New("account: account not found")

type Record struct {
	ID       uint64
	Username string
	ExternalID *string
	Nickname string
	Role string
	Status             string
	MustChangePassword bool
	CreatedAt          time.Time
	PasswordChangedAt *time.Time
}

func (r Record) Active() bool { return r.Status == string(entUser.StatusACTIVE) }

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

type CreateParams struct {
	Username string
	Nickname string
	PasswordHash string
	ExternalID *string
}

func (s *Store) Create(ctx context.Context, params CreateParams) (*Record, error) {
	if params.Username == "" || params.Nickname == "" || params.PasswordHash == "" {
		return nil, errors.New("account: username, nickname and password hash are required")
	}

	builder := s.client.User.
		Create().
		SetUsername(params.Username).
		SetNickname(params.Nickname).
		SetPasswordHash(params.PasswordHash).
		SetRole(entUser.RoleAgent).
		SetStatus(entUser.StatusACTIVE).
		SetMustChangePassword(true)
	if params.ExternalID != nil && *params.ExternalID != "" {
		builder.SetExternalID(*params.ExternalID)
	}
	record, err := builder.Save(ctx)
	if ent.IsConstraintError(err) {
		return nil, ErrUsernameTaken
	}
	if err != nil {
		return nil, err
	}

	return fromEnt(record), nil
}


// List 按创建时间倒序分页列出账号。
//
// keyword 非空时按用户名或昵称做大小写不敏感的子串匹配；匹配走 ent 的 ContainsFold，
// 它会转义用户输入里的 % 与 _，所以搜 "a_b" 不会命中 "axb"。
//
// ⚠️ page / pageSize 由调用方归一（口径见 transport 层的 httpx.NormalizePage）：
// 这里只负责按给定值算 offset，不再兜底一次。单页条数上限是 HTTP 层的约束，
// 仓储层不该有自己的第二套默认值。
//
// 返回的 total 是**过滤后**的命中总数（keyword 生效之后还有多少条），与
// len(records) 只有在「一页装得下」时才相等；拿 len(records) 去算页数会少算。
func (s *Store) List(ctx context.Context, keyword string, page, pageSize int) ([]*Record, int, error) {
	query := s.client.User.Query()
	if keyword != "" {
		query = query.Where(
			entUser.Or(
				entUser.UsernameContainsFold(keyword),
				entUser.NicknameContainsFold(keyword),
			),
		)
	}

	// Count 必须在加 Order / Offset 之前问：那两者会改到查询上，而总数与排序无关。
	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	records, err := query.
		Order(
			ent.Desc(entUser.FieldCreatedAt),
			ent.Desc(entUser.FieldID),
		).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}

	list := make([]*Record, 0, len(records))
	for _, record := range records {
		list = append(list, fromEnt(record))
	}

	return list, total, nil
}

func (s *Store) ByExternalID(ctx context.Context, externalID string) (*Record, error) {
	if externalID == "" {
		return nil, ErrNotFound
	}
	record, err := s.client.User.Query().Where(entUser.ExternalIDEQ(externalID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return fromEnt(record), nil
}


func (s *Store) UpdateExternalUser(ctx context.Context, id uint64,externalID, username, nickname string) error {
	affected, err := s.client.User.Update().Where(entUser.IDEQ(id)).
		SetExternalID(externalID).
		SetUsername(username).
		SetNickname(nickname).
		Save(ctx)
	if ent.IsConstraintError(err) {
		return ErrUsernameTaken
	}
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

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

func (s *Store) Update(ctx context.Context, id uint64, nickname, role, status string) (*Record, error) {
	query := s.client.User.UpdateOneID(id)
	if nickname != "" {
		query.SetNickname(nickname)
	}
	if role != "" {
		query.SetRole(entUser.Role(role))
	}
	if status != "" {
		query.SetStatus(entUser.Status(status))
	}
	record, err := query.Save(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return fromEnt(record), nil
}

func (s *Store) ResetPassword(ctx context.Context, id uint64, passwordHash string) error {
	if passwordHash == "" {
		return errors.New("account: password hash is required")
	}
	affected, err := s.client.User.Update().Where(entUser.IDEQ(id)).
		SetPasswordHash(passwordHash).
		SetMustChangePassword(true).
		ClearPasswordChangedAt().
		Save(ctx)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}


func (s *Store) MigrateAnonymousHistory(	ctx context.Context,	from string,	to string,) (int64, error) {
	if from == "" || to == "" {
		return 0, errors.New("account: migration subjects are required")
	}
	if from == to {
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
		ExternalID:         record.ExternalID,
		Nickname:           record.Nickname,
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
