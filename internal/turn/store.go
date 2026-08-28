package turn

import (
	"context"
	"errors"
	"fmt"
	"time"

	"eino-quickstart/ent"
	entchatturn "eino-quickstart/ent/chatturn"
	entsession "eino-quickstart/ent/session"
	"eino-quickstart/internal/storage/entx"

	"github.com/cloudwego/eino/schema"
)

const (
	StatusRunning     = "RUNNING"
	StatusInterrupted = "INTERRUPTED"
	StatusCompleted   = "COMPLETED"
	StatusFailed      = "FAILED"
)

var ErrNotFoundOrForbidden = errors.New(
	"chat turn not found or access denied",
)

type Record struct {
	ID               string
	SessionID        string
	OwnerSubject     string
	UserContent      string
	AssistantContent *string
	Status           string
	ApprovalID       *string
	CheckpointID     *string
	CreatedAt        time.Time
	CompletedAt      *time.Time
}

type Store struct {
	client *ent.Client
}

func NewStore(client *ent.Client) *Store {
	return &Store{
		client: client,
	}
}

// Start creates one RUNNING turn and persists its user message atomically.
func (s *Store) Start(
	ctx context.Context,
	turnID string,
	sessionID string,
	ownerSubject string,
	userContent string,
) (*Record, error) {
	var created *ent.ChatTurn

	err := entx.WithTx(ctx, s.client, func(tx *ent.Tx) error {
		sessionRecord, err := tx.Session.
			Query().
			Where(
				entsession.SessionIDEQ(sessionID),
				entsession.OwnerSubjectEQ(ownerSubject),
			).
			Only(ctx)
		if ent.IsNotFound(err) {
			return ErrNotFoundOrForbidden
		}
		if err != nil {
			return err
		}

		created, err = tx.ChatTurn.
			Create().
			SetTurnID(turnID).
			SetSessionID(sessionID).
			SetOwnerSubject(ownerSubject).
			SetUserContent(userContent).
			SetStatus(StatusRunning).
			Save(ctx)
		if err != nil {
			return err
		}

		return tx.SessionMessage.
			Create().
			SetSession(sessionRecord).
			SetRole(string(schema.User)).
			SetContent(userContent).
			Exec(ctx)
	})
	if err != nil {
		return nil, err
	}

	return toRecord(created), nil
}

// MarkInterrupted binds the turn to the approval and ADK checkpoint.
func (s *Store) MarkInterrupted(
	ctx context.Context,
	turnID string,
	ownerSubject string,
	approvalID string,
	checkpointID string,
) error {
	affected, err := s.client.ChatTurn.
		Update().
		Where(
			entchatturn.TurnIDEQ(turnID),
			entchatturn.OwnerSubjectEQ(ownerSubject),
			entchatturn.StatusEQ(StatusRunning),
		).
		SetStatus(StatusInterrupted).
		SetApprovalID(approvalID).
		SetCheckpointID(checkpointID).
		Save(ctx)
	if err != nil {
		return err
	}

	if affected != 1 {
		return fmt.Errorf(
			"chat turn %s is not running",
			turnID,
		)
	}

	return nil
}

// GetOwned gets a turn only when the requesting subject owns it.
func (s *Store) GetOwned(
	ctx context.Context,
	turnID string,
	ownerSubject string,
) (*Record, error) {
	record, err := s.client.ChatTurn.
		Query().
		Where(
			entchatturn.TurnIDEQ(turnID),
			entchatturn.OwnerSubjectEQ(ownerSubject),
		).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFoundOrForbidden
	}
	if err != nil {
		return nil, err
	}

	return toRecord(record), nil
}

// Complete writes the assistant message exactly once.
//
// Only RUNNING or INTERRUPTED turns may complete. Because the turn status and
// session message are updated in one transaction, a retry cannot add a second
// assistant message after a successful completion.
func (s *Store) Complete(
	ctx context.Context,
	turnID string,
	ownerSubject string,
	assistantContent string,
) error {
	return entx.WithTx(ctx, s.client, func(tx *ent.Tx) error {
		record, err := tx.ChatTurn.
			Query().
			Where(
				entchatturn.TurnIDEQ(turnID),
				entchatturn.OwnerSubjectEQ(ownerSubject),
			).
			Only(ctx)
		if ent.IsNotFound(err) {
			return ErrNotFoundOrForbidden
		}
		if err != nil {
			return err
		}

		if record.Status == StatusCompleted {
			return nil
		}

		if record.Status != StatusRunning &&
			record.Status != StatusInterrupted {
			return fmt.Errorf(
				"chat turn %s cannot complete from status %s",
				turnID,
				record.Status,
			)
		}

		sessionRecord, err := tx.Session.
			Query().
			Where(
				entsession.SessionIDEQ(record.SessionID),
				entsession.OwnerSubjectEQ(ownerSubject),
			).
			Only(ctx)
		if ent.IsNotFound(err) {
			return ErrNotFoundOrForbidden
		}
		if err != nil {
			return err
		}

		now := time.Now().UTC()

		affected, err := tx.ChatTurn.
			Update().
			Where(
				entchatturn.IDEQ(record.ID),
				entchatturn.StatusIn(
					StatusRunning,
					StatusInterrupted,
				),
			).
			SetStatus(StatusCompleted).
			SetAssistantContent(assistantContent).
			SetCompletedAt(now).
			Save(ctx)
		if err != nil {
			return err
		}

		if affected == 0 {
			return nil
		}

		return tx.SessionMessage.
			Create().
			SetSession(sessionRecord).
			SetRole(string(schema.Assistant)).
			SetContent(assistantContent).
			Exec(ctx)
	})
}

// Fail marks a still-running turn as failed without modifying Session history.
func (s *Store) Fail(
	ctx context.Context,
	turnID string,
	ownerSubject string,
) error {
	_, err := s.client.ChatTurn.
		Update().
		Where(
			entchatturn.TurnIDEQ(turnID),
			entchatturn.OwnerSubjectEQ(ownerSubject),
			entchatturn.StatusEQ(StatusRunning),
		).
		SetStatus(StatusFailed).
		Save(ctx)

	return err
}
func (s *Store) DeleteTerminalBefore(ctx context.Context, before time.Time) (int, error) {
	return s.client.ChatTurn.
		Delete().Where(
		entchatturn.StatusIn(
			StatusCompleted,
			StatusFailed,
		),
		entchatturn.CompletedAtLT(before),
	).Exec(ctx)
}

func toRecord(record *ent.ChatTurn) *Record {
	return &Record{
		ID:               record.TurnID,
		SessionID:        record.SessionID,
		OwnerSubject:     record.OwnerSubject,
		UserContent:      record.UserContent,
		AssistantContent: record.AssistantContent,
		Status:           record.Status.String(),
		ApprovalID:       record.ApprovalID,
		CheckpointID:     record.CheckpointID,
		CreatedAt:        record.CreatedAt,
		CompletedAt:      record.CompletedAt,
	}
}
