package turn

import (
	"context"
	"errors"
	"fmt"
	"time"

	"eino-quickstart/ent"
	entchatturn "eino-quickstart/ent/chatturn"
	entsession "eino-quickstart/ent/session"
	"eino-quickstart/internal/platform/storage/entx"

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
func (s *Store) Start(ctx context.Context, turnID string, sessionID string, ownerSubject string, userContent string) (*Record, error) {
	var created *ent.ChatTurn

	err := entx.WithTx(ctx, s.client, func(tx *ent.Tx) error {
		sessionRecord, err := tx.Session.
			Query().Where(
			entsession.SessionIDEQ(sessionID),
			entsession.OwnerSubjectEQ(ownerSubject),
		).Only(ctx)
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

		if err := touchSession(ctx, tx, sessionRecord.ID); err != nil {
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
//
// An empty assistantContent still completes the turn but appends nothing to the
// session history — see the branch below for why that is not merely a nicety.
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

		if err := touchSession(ctx, tx, sessionRecord.ID); err != nil {
			return err
		}

		// 空回答：轮次照样收成 COMPLETED，但不往会话历史里塞一条空消息。
		//
		// 空回答是真实存在的：模型调完工具直接结束、没输出任何文本。不能写的
		// 理由有两条 —— session_messages.content 是 NotEmpty，塞空串会触发 ent
		// 的校验错误，让**整个事务回滚**（轮次就永远卡在 RUNNING 了，而且这是
		// 静默的：调用方只看到一个校验错误）；就算写得进去，它也会作为一段空
		// 上下文被重新喂给模型。
		if assistantContent == "" {
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

// touchSession 刷新会话的最后活动时间。
//
// 放在轮次的同一个事务里、而不是让调用方补一句，是因为「消息进了会话历史」
// 与「会话浮到列表顶部」必须同时成立 —— 分开写会出现「消息写进去了、会话却还
// 排在下面」的半截状态，而那个状态没有任何自愈路径。
//
// 只有 Start 与 Complete 两处调用，这是刻意的：它们才是「会话真的动了」的两个
// 时刻。MarkInterrupted（工具调用等审批）不调 —— 它紧跟在 Start 之后，会话的
// 最后活动时间本来就该是那个 Start 的时刻。
func touchSession(ctx context.Context, tx *ent.Tx, sessionID uint64) error {
	_, err := tx.Session.
		UpdateOneID(sessionID).
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)

	return err
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
func (s *Store) DeleteTerminalBefore(
	ctx context.Context,
	before time.Time,
	limit int,
) (int, error) {
	if limit <= 0 {
		return 0, fmt.Errorf("delete limit must be greater than zero")
	}
	records, err := s.client.ChatTurn.
		Query().
		Where(
			entchatturn.StatusIn(
				StatusCompleted,
				StatusFailed,
			),
			entchatturn.CompletedAtLT(before),
		).
		Limit(limit).
		All(ctx)
	if err != nil {
		return 0, err
	}
	if len(records) == 0 {
		return 0, nil
	}

	ids := make([]uint64, len(records))
	for i, record := range records {
		ids[i] = record.ID
	}

	return s.client.ChatTurn.
		Delete().Where(
		entchatturn.IDIn(ids...),
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
