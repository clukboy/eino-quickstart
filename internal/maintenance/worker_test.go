package maintenance

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

type fakeTurnCleaner struct {
	count   int
	deleted int
	mutated bool
}

func (f *fakeTurnCleaner) CountTerminalBefore(context.Context, time.Time) (int, error) {
	return f.count, nil
}

func (f *fakeTurnCleaner) DeleteTerminalBefore(context.Context, time.Time, int) (int, error) {
	f.mutated = true
	return f.deleted, nil
}

func testWorker(cleaner *fakeTurnCleaner) *Worker {
	return &Worker{
		Turns:         cleaner,
		TurnRetention: time.Hour,
		BatchSize:     10,
		Logger:        slog.Default(),
	}
}

func TestPreviewIsReadOnlyAndReportsCounts(t *testing.T) {
	cleaner := &fakeTurnCleaner{count: 5, deleted: 3}
	report, err := testWorker(cleaner).Preview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !report.DryRun || report.Total != 5 || cleaner.mutated {
		t.Fatalf("report=%+v mutated=%v", report, cleaner.mutated)
	}
}

func TestRunOnceReportsMutationCounts(t *testing.T) {
	cleaner := &fakeTurnCleaner{count: 5, deleted: 3}
	report, err := testWorker(cleaner).RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.DryRun || report.Total != 3 || !cleaner.mutated {
		t.Fatalf("report=%+v mutated=%v", report, cleaner.mutated)
	}
}

func TestRunExecutesInitialReportAndStopsOnCancel(t *testing.T) {
	cleaner := &fakeTurnCleaner{deleted: 1}
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
	worker := testWorker(&fakeTurnCleaner{})
	if err := worker.Run(context.Background()); err == nil {
		t.Fatal("expected invalid interval error")
	}
}
