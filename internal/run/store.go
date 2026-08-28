package run

import (
	"context"
	"fmt"
	"time"

	"eino-quickstart/ent"
	entagentrun "eino-quickstart/ent/agentrun"
)

const (
	StatusRunning     = "RUNNING"
	StatusInterrupted = "INTERRUPTED"
	StatusResuming    = "RESUMING"
	StatusCompleted   = "COMPLETED"
	StatusFailed      = "FAILED"
	StatusExpired     = "EXPIRED"
)

type Record struct {
	RunID        string
	SessionID    string
	RequestedBy  string
	UserMessage  string
	CheckpointID *string
	ApprovalID   *string
	Status       string
	ErrorCode    *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	CompletedAt  *time.Time
	ExpiresAt    *time.Time
}

type Store struct {
	client *ent.Client
}

func NewStore(client *ent.Client) *Store {
	return &Store{client: client}
}

func (s *Store) Create(
	ctx context.Context,
	runID string,
	sessionID string,
	requestedBy string,
	userMessage string,
	expiresAt time.Time,
) error {
	return s.client.AgentRun.
		Create().
		SetRunID(runID).
		SetSessionID(sessionID).
		SetRequestedBy(requestedBy).
		SetUserMessage(userMessage).
		SetCheckpointID(runID).
		SetStatus(StatusRunning).
		SetExpiresAt(expiresAt).
		Exec(ctx)
}

func (s *Store) MarkInterrupted(
	ctx context.Context,
	runID string,
	approvalID string,
) error {
	affected, err := s.client.AgentRun.
		Update().
		Where(
			entagentrun.RunIDEQ(runID),
			entagentrun.StatusEQ(StatusRunning),
		).
		SetStatus(StatusInterrupted).
		SetApprovalID(approvalID).
		Save(ctx)
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("run %s is not running", runID)
	}
	return nil
}

func (s *Store) ClaimResume(
	ctx context.Context,
	runID string,
	requestedBy string,
) (*Record, error) {
	record, err := s.client.AgentRun.
		Query().
		Where(
			entagentrun.RunIDEQ(runID),
		).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, fmt.Errorf("run not found")
	}
	if err != nil {
		return nil, err
	}

	if record.RequestedBy != requestedBy {
		return nil, fmt.Errorf("run not found")
	}
	if record.Status != StatusInterrupted {
		return nil, fmt.Errorf(
			"run %s cannot resume from status %s",
			runID,
			record.Status,
		)
	}
	if record.ExpiresAt != nil &&
		!record.ExpiresAt.After(time.Now().UTC()) {
		return nil, fmt.Errorf("run %s has expired", runID)
	}

	affected, err := s.client.AgentRun.
		Update().
		Where(
			entagentrun.IDEQ(record.ID),
			entagentrun.StatusEQ(StatusInterrupted),
		).
		SetStatus(StatusResuming).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if affected != 1 {
		return nil, fmt.Errorf("run %s is already resuming", runID)
	}

	record.Status = StatusResuming
	return toRecord(record), nil
}

func (s *Store) MarkCompleted(
	ctx context.Context,
	runID string,
) error {
	now := time.Now().UTC()

	affected, err := s.client.AgentRun.
		Update().
		Where(
			entagentrun.RunIDEQ(runID),
			entagentrun.StatusIn(
				StatusRunning,
				StatusResuming,
			),
		).
		SetStatus(StatusCompleted).
		SetCompletedAt(now).
		Save(ctx)
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("run %s cannot complete", runID)
	}

	return nil
}

func (s *Store) MarkFailed(
	ctx context.Context,
	runID string,
	errorCode string,
) error {
	_, err := s.client.AgentRun.
		Update().
		Where(
			entagentrun.RunIDEQ(runID),
			entagentrun.StatusIn(
				StatusRunning,
				StatusResuming,
			),
		).
		SetStatus(StatusFailed).
		SetErrorCode(errorCode).
		Save(ctx)

	return err
}

func (s *Store) ReleaseResume(
	ctx context.Context,
	runID string,
) error {
	affected, err := s.client.AgentRun.
		Update().
		Where(
			entagentrun.RunIDEQ(runID),
			entagentrun.StatusEQ(StatusResuming),
		).
		SetStatus(StatusInterrupted).
		Save(ctx)
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("run %s is not resuming", runID)
	}

	return nil
}

func toRecord(record *ent.AgentRun) *Record {
	return &Record{
		RunID:        record.RunID,
		SessionID:    record.SessionID,
		RequestedBy:  record.RequestedBy,
		UserMessage:  record.UserMessage,
		CheckpointID: record.CheckpointID,
		ApprovalID:   record.ApprovalID,
		Status:       record.Status.String(),
		ErrorCode:    record.ErrorCode,
		CreatedAt:    record.CreatedAt,
		UpdatedAt:    record.UpdatedAt,
		CompletedAt:  record.CompletedAt,
		ExpiresAt:    record.ExpiresAt,
	}
}
