package maintenance

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

type fakeCleaner struct {
	approvalExpired int
	approvalDeleted int
	checkpoint      int
	turn            int
	mutated         bool
}

func (f *fakeCleaner) ExpirePending(context.Context, time.Time, int) ([]string, error) {
	f.mutated = true
	ids := make([]string, f.approvalExpired)
	return ids, nil
}
func (f *fakeCleaner) DeleteTerminalBefore(context.Context, time.Time, int) (int, error) {
	f.mutated = true
	return f.approvalDeleted, nil
}
func (f *fakeCleaner) CountExpiredPending(context.Context, time.Time) (int, error) {
	return f.approvalExpired, nil
}
func (f *fakeCleaner) CountTerminalBefore(context.Context, time.Time) (int, error) {
	return f.approvalDeleted, nil
}
func (f *fakeCleaner) DeleteExpired(context.Context, time.Time, int) ([]string, error) {
	f.mutated = true
	ids := make([]string, f.checkpoint)
	return ids, nil
}
func (f *fakeCleaner) CountExpired(context.Context, time.Time) (int, error) { return f.checkpoint, nil }
func (f *fakeCleaner) CountTerminalBeforeTurns(context.Context, time.Time) (int, error) {
	return f.turn, nil
}
func (f *fakeCleaner) DeleteTerminalBeforeTurns(context.Context, time.Time, int) (int, error) {
	f.mutated = true
	return f.turn, nil
}

type fakeTurnCleaner struct{ *fakeCleaner }

func (f fakeTurnCleaner) DeleteTerminalBefore(ctx context.Context, before time.Time, limit int) (int, error) {
	return f.DeleteTerminalBeforeTurns(ctx, before, limit)
}
func (f fakeTurnCleaner) CountTerminalBefore(ctx context.Context, before time.Time) (int, error) {
	return f.CountTerminalBeforeTurns(ctx, before)
}

func testWorker(f *fakeCleaner) *Worker {
	return &Worker{
		Approvals: f, Checkpoints: f, Turns: fakeTurnCleaner{f},
		ApprovalRetention: time.Hour, CheckpointRetention: time.Hour, TurnRetention: time.Hour,
		BatchSize: 10, Logger: slog.Default(),
	}
}

func TestPreviewIsReadOnlyAndReportsCounts(t *testing.T) {
	cleaner := &fakeCleaner{approvalExpired: 2, approvalDeleted: 3, checkpoint: 4, turn: 5}
	report, err := testWorker(cleaner).Preview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !report.DryRun || report.Total != 14 || cleaner.mutated {
		t.Fatalf("report=%+v mutated=%v", report, cleaner.mutated)
	}
}

func TestRunOnceReportsMutationCounts(t *testing.T) {
	cleaner := &fakeCleaner{approvalExpired: 2, approvalDeleted: 3, checkpoint: 4, turn: 5}
	report, err := testWorker(cleaner).RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.DryRun || report.Total != 14 || !cleaner.mutated {
		t.Fatalf("report=%+v mutated=%v", report, cleaner.mutated)
	}
}

func TestRunExecutesInitialReportAndStopsOnCancel(t *testing.T) {
	cleaner := &fakeCleaner{approvalExpired: 1}
	reports := make(chan Report, 1)
	worker := testWorker(cleaner)
	worker.Interval = time.Hour
	worker.OnReport = func(report Report) { reports <- report }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	select {
	case report := <-reports:
		if report.Total != 1 {
			t.Fatalf("report=%+v", report)
		}
	case <-time.After(time.Second):
		t.Fatal("maintenance worker did not run immediately")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("maintenance worker did not stop after cancellation")
	}
}

func TestRunRejectsInvalidSchedule(t *testing.T) {
	worker := testWorker(&fakeCleaner{})
	if err := worker.Run(context.Background()); err == nil {
		t.Fatal("expected invalid interval error")
	}
}
