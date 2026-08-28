package session

import (
	"context"
	"eino-quickstart/ent"
	entSession "eino-quickstart/ent/session"
	"eino-quickstart/ent/sessionmessage"
	"errors"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	_ "github.com/lib/pq"
)

var ErrNotFoundOrForbidden = errors.New(
	"session not found or access denied",
)

type Session struct {
	ID        string            `json:"id"`
	Messages  []*schema.Message `json:"-"`
	CreateAt  time.Time         `json:"createAt"`
	UpdatedAt time.Time         `json:"updatedAt"`
	Mu        sync.RWMutex
}

type Store struct {
	client *ent.Client
}

func NewStore(client *ent.Client) *Store {
	return &Store{client: client}
}
func (s *Store) GetOrCreate(ctx context.Context, id string, subject string) error {
	_, err := s.getOrCreate(ctx, id, subject)
	return err
}

func (s *Store) History(ctx context.Context, sessionID string, subject string) ([]*schema.Message, error) {
	records, err := s.client.SessionMessage.Query().Where(sessionmessage.HasSessionWith(
		entSession.SessionIDEQ(sessionID),
		entSession.OwnerSubjectEQ(subject),
	)).Order(
		ent.Asc(sessionmessage.FieldCreatedAt),
		ent.Asc(sessionmessage.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	messages := make([]*schema.Message, len(records))
	for i, r := range records {
		messages[i] = &schema.Message{
			Role:    schema.RoleType(r.Role),
			Content: r.Content,
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
	if !ent.IsNotFound(err) {
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

	return s.client.Session.
		Query().
		Where(entSession.SessionIDEQ(id)).
		Only(ctx)
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
