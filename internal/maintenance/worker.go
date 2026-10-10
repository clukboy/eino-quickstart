package maintenance

import (
	"context"
	"errors"
	"fmt"
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

// Preview interfaces keep dry-run read-only. Stores may implement these beside
// their mutating cleaner interface; Worker refuses a dry-run if one is absent.
type ApprovalPreview interface {
	CountExpiredPending(ctx context.Context, now time.Time) (int, error)
	CountTerminalBefore(ctx context.Context, before time.Time) (int, error)
}
type CheckpointPreview interface {
	CountExpired(ctx context.Context, before time.Time) (int, error)
}
type TurnPreview interface {
	CountTerminalBefore(ctx context.Context, before time.Time) (int, error)
}

type Report struct {
	DryRun            bool      `json:"dry_run"`
	At                time.Time `json:"at"`
	ApprovalExpired   int       `json:"approval_expired"`
	ApprovalDeleted   int       `json:"approval_deleted"`
	CheckpointDeleted int       `json:"checkpoint_deleted"`
	TurnDeleted       int       `json:"turn_deleted"`
	Total             int       `json:"total"`
}

func (r Report) addTotal() Report {
	r.Total = r.ApprovalExpired + r.ApprovalDeleted + r.CheckpointDeleted + r.TurnDeleted
	return r
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
	// OnReport is called after every successful periodic cleanup. It is useful
	// for the standalone maintenance command to persist the latest report.
	OnReport func(Report)
}

func (w *Worker) logger() *slog.Logger {
	if w.Logger != nil {
		return w.Logger
	}
	return slog.Default()
}

func (w *Worker) Run(ctx context.Context) error {
	if w.Interval <= 0 {
		return fmt.Errorf("maintenance: interval must be greater than zero")
	}
	if w.BatchSize <= 0 {
		return fmt.Errorf("maintenance: batch size must be greater than zero")
	}
	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()

	run := func() {
		report, err := w.RunOnce(ctx)
		if err != nil {
			w.logger().Error("maintenance cleanup failed", "error", err)
			return
		}
		if w.OnReport != nil {
			w.OnReport(report)
		}
	}
	run()

	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}
			return ctx.Err()

		case <-ticker.C:
			run()
		}
	}
}

// Preview reports what the next cleanup would touch without mutating data.
func (w *Worker) Preview(ctx context.Context) (Report, error) {
	approvals, ok := w.Approvals.(ApprovalPreview)
	if !ok {
		return Report{}, fmt.Errorf("maintenance: approval cleaner does not support dry-run")
	}
	checkpoints, ok := w.Checkpoints.(CheckpointPreview)
	if !ok {
		return Report{}, fmt.Errorf("maintenance: checkpoint cleaner does not support dry-run")
	}
	turns, ok := w.Turns.(TurnPreview)
	if !ok {
		return Report{}, fmt.Errorf("maintenance: turn cleaner does not support dry-run")
	}
	now := time.Now().UTC()
	approvalExpired, err := approvals.CountExpiredPending(ctx, now)
	if err != nil {
		return Report{}, err
	}
	approvalDeleted, err := approvals.CountTerminalBefore(ctx, now.Add(-w.ApprovalRetention))
	if err != nil {
		return Report{}, err
	}
	checkpointDeleted, err := checkpoints.CountExpired(ctx, now.Add(-w.CheckpointRetention))
	if err != nil {
		return Report{}, err
	}
	turnDeleted, err := turns.CountTerminalBefore(ctx, now.Add(-w.TurnRetention))
	if err != nil {
		return Report{}, err
	}
	return (Report{DryRun: true, At: now, ApprovalExpired: approvalExpired, ApprovalDeleted: approvalDeleted, CheckpointDeleted: checkpointDeleted, TurnDeleted: turnDeleted}).addTotal(), nil
}

func (w *Worker) RunOnce(ctx context.Context) (Report, error) {
	if w.BatchSize <= 0 {
		return Report{}, fmt.Errorf("maintenance: batch size must be greater than zero")
	}
	now := time.Now().UTC()
	report := Report{At: now}

	expired, err := w.Approvals.ExpirePending(ctx, now, w.BatchSize)
	if err != nil {
		return report, err
	}
	report.ApprovalExpired = len(expired)
	for _, approvalID := range expired {
		w.logger().Info("approval expired", "approval_id", approvalID)
	}

	deleted, err := w.Checkpoints.DeleteExpired(ctx, now.Add(-w.CheckpointRetention), w.BatchSize)
	if err != nil {
		return report, err
	}
	report.CheckpointDeleted = len(deleted)

	report.ApprovalDeleted, err = w.Approvals.DeleteTerminalBefore(ctx, now.Add(-w.ApprovalRetention), w.BatchSize)
	if err != nil {
		return report, err
	}
	report.TurnDeleted, err = w.Turns.DeleteTerminalBefore(ctx, now.Add(-w.TurnRetention), w.BatchSize)
	if err != nil {
		return report, err
	}
	return report.addTotal(), nil
}
