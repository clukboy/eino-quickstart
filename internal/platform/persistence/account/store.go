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


func (s *Store) List(ctx context.Context, keyword string) ([]*Record, error) {
	query := s.client.User.Query()
	if keyword != "" {
		query = query.Where(
			entUser.Or(
				entUser.UsernameContainsFold(keyword),
				entUser.NicknameContainsFold(keyword),
			),
		)
	}

	records, err := query.
		Order(
			ent.Desc(entUser.FieldCreatedAt),
			ent.Desc(entUser.FieldID),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}

	list := make([]*Record, 0, len(records))
	for _, record := range records {
		list = append(list, fromEnt(record))
	}

	return list, nil
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
