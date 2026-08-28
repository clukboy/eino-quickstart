package maintenance

import (
	"context"
	"log/slog"
	"time"
)

type ApprovalCleaner interface {
	ExpirePending(ctx context.Context, now time.Time, limit int) ([]string, error)

	DeleteTerminalBefore(ctx context.Context, before time.Time, limit int) (int, error)
}

type CheckpointCleaner interface {
	DeleteExpired(ctx context.Context, before time.Time, limit int) ([]string, error)
}

type TurnCleaner interface {
	DeleteTerminalBefore(ctx context.Context, before time.Time, limit int) (int, error)
}

type Worker struct {
	Approvals           ApprovalCleaner
	Checkpoints         CheckpointCleaner
	Turns               TurnCleaner
	Interval            time.Duration
	ApprovalRetention   time.Duration
	CheckpointRetention time.Duration
	TurnRetention       time.Duration
	BatchSize           int
	Logger              *slog.Logger
}

func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()

	if err := w.RunOnce(ctx); err != nil {
		w.Logger.Error("initial maintenance cleanup failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			if err := w.RunOnce(ctx); err != nil {
				w.Logger.Error(
					"maintenance cleanup failed",
					"error",
					err,
				)
			}
		}
	}
}

func (w *Worker) RunOnce(ctx context.Context) error {
	now := time.Now().UTC()

	expired, err := w.Approvals.ExpirePending(
		ctx,
		now,
		w.BatchSize,
	)
	if err != nil {
		return err
	}

	for _, approvalID := range expired {
		w.Logger.Info(
			"approval expired",
			"approval_id",
			approvalID,
		)
	}

	if _, err := w.Checkpoints.DeleteExpired(
		ctx,
		now.Add(-w.CheckpointRetention),
		w.BatchSize,
	); err != nil {
		return err
	}

	if _, err := w.Approvals.DeleteTerminalBefore(
		ctx,
		now.Add(-w.ApprovalRetention),
		w.BatchSize,
	); err != nil {
		return err
	}

	_, err = w.Turns.DeleteTerminalBefore(
		ctx,
		now.Add(-w.TurnRetention),
		w.BatchSize,
	)
	return err
}
