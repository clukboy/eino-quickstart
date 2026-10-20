package maintenance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type TurnCleaner interface {
	DeleteTerminalBefore(ctx context.Context, before time.Time, limit int) (int, error)
}

// TurnPreview keeps dry-run read-only. Stores implement it beside the mutating
// cleaner; Worker refuses a dry-run if it is absent.
type TurnPreview interface {
	CountTerminalBefore(ctx context.Context, before time.Time) (int, error)
}

type Report struct {
	DryRun      bool      `json:"dry_run"`
	At          time.Time `json:"at"`
	TurnDeleted int       `json:"turn_deleted"`
	Total       int       `json:"total"`
}

func (r Report) addTotal() Report {
	r.Total = r.TurnDeleted
	return r
}

type Worker struct {
	Turns         TurnCleaner
	Interval      time.Duration
	TurnRetention time.Duration
	BatchSize     int
	Logger        *slog.Logger
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
	turns, ok := w.Turns.(TurnPreview)
	if !ok {
		return Report{}, fmt.Errorf("maintenance: turn cleaner does not support dry-run")
	}
	now := time.Now().UTC()
	turnDeleted, err := turns.CountTerminalBefore(ctx, now.Add(-w.TurnRetention))
	if err != nil {
		return Report{}, err
	}
	return (Report{DryRun: true, At: now, TurnDeleted: turnDeleted}).addTotal(), nil
}

func (w *Worker) RunOnce(ctx context.Context) (Report, error) {
	if w.BatchSize <= 0 {
		return Report{}, fmt.Errorf("maintenance: batch size must be greater than zero")
	}
	now := time.Now().UTC()
	report := Report{At: now}

	deleted, err := w.Turns.DeleteTerminalBefore(ctx, now.Add(-w.TurnRetention), w.BatchSize)
	if err != nil {
		return report, err
	}
	report.TurnDeleted = deleted
	return report.addTotal(), nil
}
