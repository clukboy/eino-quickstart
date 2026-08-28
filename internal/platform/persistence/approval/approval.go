package approval

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/ent/approval"
	"eino-quickstart/internal/platform/privacy"
	"eino-quickstart/internal/platform/storage/entx"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	StatusPending  = "PENDING"
	StatusApproved = "APPROVED"
	StatusRejected = "REJECTED"
	StatusResuming = "RESUMING"
	StatusExecuted = "EXECUTED"
	StatusExpired  = "EXPIRED"
)

type Request struct {
	ID               string     `json:"id"`
	SessionID        string     `json:"sessionId"`
	RunID            *string    `json:"-"`
	TurnID           *string    `json:"-"`
	Tool             string     `json:"tool"`
	ArgumentsHash    *string    `json:"-"`
	DisplayArguments *string    `json:"displayArguments"`
	Status           string     `json:"status"`
	RequestedBy      string     `json:"requestedBy"`
	DecidedBy        *string    `json:"decidedBy"`
	CheckpointID     *string    `json:"checkpointId,omitempty"`
	InterruptID      *string    `json:"interruptId,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	ExpiresAt        *time.Time `json:"expiresAt,omitempty"`
}
type Store struct {
	client         *ent.Client
	argumentPolicy privacy.ArgumentPolicy
	approvalTTL    time.Duration
}

func NewStore(client *ent.Client, argumentPolicy privacy.ArgumentPolicy, approvalTTL time.Duration) *Store {
	return &Store{client: client, argumentPolicy: argumentPolicy, approvalTTL: approvalTTL}
}

func (s *Store) Create(ctx context.Context, sessionID, turnID, actorSubject, tool, args string) (string, error) {
	var record *ent.Approval
	prepared, err := s.argumentPolicy.Prepare(args)
	if err != nil {
		return "", fmt.Errorf("prepare approval arguments: %w", err)
	}
	err = entx.WithTx(ctx, s.client, func(tx *ent.Tx) error {
		var err error
		expiresAt := time.Now().Add(s.approvalTTL)
		record, err = tx.Approval.Create().
			SetApprovalID(uuid.NewString()).
			SetSessionID(sessionID).
			SetTurnID(turnID).
			SetTool(tool).
			SetArgumentsHash(prepared.Hash).
			SetDisplayArguments(prepared.Display).
			SetRequestedBy(actorSubject).
			SetStatus(StatusPending).
			SetExpiresAt(expiresAt).
			Save(ctx)
		if err != nil {
			return err
		}
		return writeAudit(ctx, tx, sessionID, record.ApprovalID, "approval.created", tool, prepared.Display, actorSubject)
	})
	if err != nil {
		return "", err
	}
	return record.ApprovalID, nil
}

func (s *Store) AttachCheckpoint(ctx context.Context, approvalID, runId, checkpointID, interruptID string) error {
	if checkpointID == "" {
		return fmt.Errorf("checkpoint ID is required")
	}
	if interruptID == "" {
		return fmt.Errorf("interrupt ID is required")
	}

	affected, err := s.client.Approval.
		Update().
		Where(
			approval.ApprovalIDEQ(approvalID),
			approval.StatusEQ(StatusPending),
			approval.CheckpointIDIsNil(),
			approval.InterruptIDIsNil(),
		).
		SetCheckpointID(checkpointID).
		SetInterruptID(interruptID).
		SetRunID(runId).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("attach checkpoint: %w", err)
	}

	if affected != 1 {
		return fmt.Errorf(
			"approval %s is not pending or already has a checkpoint",
			approvalID,
		)
	}

	return nil
}

func (s *Store) Get(ctx context.Context, id string) (*Request, bool) {
	record, err := s.client.Approval.Query().Where(approval.ApprovalIDEQ(id)).Only(ctx)
	if err != nil {
		return nil, false
	}
	return &Request{
		ID:               record.ApprovalID,
		SessionID:        record.SessionID,
		Tool:             record.Tool,
		ArgumentsHash:    record.ArgumentsHash,
		DisplayArguments: record.DisplayArguments,
		Status:           record.Status.String(),
		CreatedAt:        record.CreatedAt,
	}, true
}

func (s *Store) Decide(ctx context.Context, id string, approved bool, actorSubject string) error {
	status := StatusRejected
	eventType := "approval.rejected"
	if approved {
		status = StatusApproved
		eventType = "approval.approved"
	}

	return entx.WithTx(ctx, s.client, func(tx *ent.Tx) error {
		affected, err := tx.Approval.Update().Where(
			approval.ApprovalIDEQ(id),
			approval.StatusEQ(StatusPending),
			approval.ExpiresAtGT(time.Now()),
		).SetStatus(approval.Status(status)).
			SetDecidedBy(actorSubject).
			SetDecidedAt(time.Now()).
			Save(ctx)
		if err != nil {
			return err
		}
		if affected != 1 {
			return fmt.Errorf("approval %s not pending", id)
		}
		record, err := tx.Approval.
			Query().
			Where(approval.ApprovalIDEQ(id)).
			Only(ctx)
		if err != nil {
			return err
		}
		if err := writeAudit(
			ctx,
			tx,
			record.SessionID,
			record.ApprovalID,
			eventType,
			record.Tool,
			"",
			actorSubject,
		); err != nil {
			return err
		}
		return nil
	})
}

func (s *Store) Consume(ctx context.Context, id, sessionID, actorSubject, toolName, args string) error {
	record, err := s.client.Approval.Query().Where(approval.ApprovalIDEQ(id)).Only(ctx)
	if err != nil {
		return err
	}
	prepared, err := s.argumentPolicy.Prepare(args)
	if err != nil {
		return fmt.Errorf("prepare approval arguments: %w", err)
	}
	if record.Status != StatusResuming {
		return fmt.Errorf("approval %s not resuming (status=%s)", id, record.Status)
	}
	if record.SessionID != sessionID {
		return fmt.Errorf("approval %s belongs to another session", id)
	}
	if record.Tool != toolName {
		return fmt.Errorf("approval %s belongs to another tool", id)
	}
	if record.ArgumentsHash != nil && *record.ArgumentsHash != prepared.Hash {
		return fmt.Errorf("approval %s belongs to another arguments", id)
	}
	if record.RequestedBy != actorSubject {
		return fmt.Errorf("approval %s not requested by %s", id, actorSubject)
	}

	return entx.WithTx(ctx, s.client, func(tx *ent.Tx) error {
		affected, err := tx.Approval.Update().Where(
			approval.ApprovalIDEQ(id),
			approval.StatusEQ(StatusResuming),
			approval.SessionIDEQ(sessionID),
			approval.RequestedByEQ(actorSubject),
			approval.ToolEQ(toolName),
			approval.ArgumentsHashEQ(prepared.Hash),
		).SetStatus(StatusExecuted).
			SetExecutedAt(time.Now()).
			Save(ctx)
		if err != nil {
			return err
		}
		if affected != 1 {
			return fmt.Errorf("approval %s not pending", id)
		}
		if err := writeAudit(
			ctx,
			tx,
			record.SessionID,
			record.ApprovalID,
			"approval.executed",
			record.Tool,
			prepared.Display,
			actorSubject,
		); err != nil {
			return err
		}
		return nil
	})

}

func (s *Store) ClaimForResume(ctx context.Context, approvalID string, requestedBy string) (*Request, error) {
	var record *ent.Approval
	err := entx.WithTx(ctx, s.client, func(tx *ent.Tx) error {
		item, err := tx.Approval.
			Query().
			Where(
				approval.ApprovalIDEQ(approvalID),
			).
			Only(ctx)
		if err != nil {
			return err
		}

		if item.RequestedBy != requestedBy {
			return fmt.Errorf("approval not found")
		}

		if item.Status != StatusApproved {
			return fmt.Errorf(
				"approval %s cannot resume from status %s",
				approvalID,
				item.Status,
			)
		}

		if item.ExpiresAt != nil &&
			!item.ExpiresAt.After(time.Now().UTC()) {
			return fmt.Errorf("approval %s has expired", approvalID)
		}

		if item.CheckpointID == nil ||
			item.InterruptID == nil {
			return fmt.Errorf(
				"approval %s has no resumable checkpoint",
				approvalID,
			)
		}

		if item.RunID == nil {
			return fmt.Errorf(
				"approval %s is not linked to an agent run",
				approvalID,
			)
		}

		affected, err := tx.Approval.
			Update().
			Where(
				approval.IDEQ(item.ID),
				approval.StatusEQ(StatusApproved),
			).
			SetStatus(StatusResuming).
			Save(ctx)
		if err != nil {
			return err
		}

		if affected != 1 {
			return fmt.Errorf(
				"approval %s is already being resumed",
				approvalID,
			)
		}

		record = item
		record.Status = StatusResuming

		return writeAudit(
			ctx,
			tx,
			item.SessionID,
			item.ApprovalID,
			"approval.resuming",
			item.Tool,
			"",
			requestedBy,
		)
	})
	if err != nil {
		return nil, err
	}

	return toRequest(record), nil
}

func (s *Store) ReleaseResume(ctx context.Context, approvalID string) error {
	affected, err := s.client.Approval.
		Update().
		Where(
			approval.ApprovalIDEQ(approvalID),
			approval.StatusEQ(StatusResuming),
		).
		SetStatus(StatusApproved).
		Save(ctx)
	if err != nil {
		return err
	}

	if affected != 1 {
		return fmt.Errorf(
			"approval %s is not being resumed",
			approvalID,
		)
	}

	return nil
}
func writeAudit(ctx context.Context, tx *ent.Tx, sessionID, approvalID, eventType, toolName, payload, actorSubject string) error {
	return tx.AuditEvent.Create().SetSessionID(sessionID).SetApprovalID(approvalID).SetEventType(eventType).SetTool(toolName).SetPayload(payload).SetActorSubject(actorSubject).Exec(ctx)
}

func (s *Store) MarkResuming(ctx context.Context, approvalID string) error {
	affected, err := s.client.Approval.
		Update().
		Where(
			approval.ApprovalIDEQ(approvalID),
			approval.StatusEQ(StatusApproved),
		).
		SetStatus(StatusResuming).
		Save(ctx)
	if err != nil {
		return err
	}

	if affected != 1 {
		return fmt.Errorf(
			"approval %s is already being resumed or is not approved",
			approvalID,
		)
	}

	return nil
}

func (s *Store) ResetResuming(ctx context.Context, approvalID string) error {
	_, err := s.client.Approval.
		Update().
		Where(
			approval.ApprovalIDEQ(approvalID),
			approval.StatusEQ(StatusResuming),
		).
		SetStatus(StatusApproved).
		Save(ctx)

	return err
}

func (s *Store) ExpirePending(ctx context.Context, now time.Time, limit int) ([]string, error) {
	records, err := s.client.Approval.
		Query().Where(
		approval.StatusEQ(StatusPending),
		approval.ExpiresAtLT(now),
	).Limit(limit).All(ctx)
	if err != nil {
		return nil, err
	}

	expired := make([]string, 0, len(records))

	for _, record := range records {
		affected, err := s.client.Approval.
			Update().Where(
			approval.IDEQ(record.ID),
			approval.StatusEQ(StatusPending),
			approval.ExpiresAtLT(now),
		).SetStatus(StatusExpired).Save(ctx)
		if err != nil {
			return nil, err
		}

		if affected == 1 {
			expired = append(expired, record.ApprovalID)
		}
	}

	return expired, nil
}

func (s *Store) DeleteTerminalBefore(ctx context.Context, before time.Time, limit int) (int, error) {
	return s.client.Approval.
		Delete().Where(
		approval.StatusIn(
			StatusRejected,
			StatusExpired,
			StatusExecuted,
		),
		approval.DecidedAtLT(before),
	).Exec(ctx)
}

func toRequest(record *ent.Approval) *Request {
	return &Request{
		ID:               record.ApprovalID,
		SessionID:        record.SessionID,
		RunID:            record.RunID,
		TurnID:           record.TurnID,
		Tool:             record.Tool,
		ArgumentsHash:    record.ArgumentsHash,
		DisplayArguments: record.DisplayArguments,
		Status:           record.Status.String(),
		CreatedAt:        record.CreatedAt,
		RequestedBy:      record.RequestedBy,
		DecidedBy:        record.DecidedBy,
		CheckpointID:     record.CheckpointID,
		InterruptID:      record.InterruptID,
		ExpiresAt:        record.ExpiresAt,
	}
}
