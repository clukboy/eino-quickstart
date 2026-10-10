package approval

import (
	"context"
	"strings"
	"testing"
	"time"

	"eino-quickstart/ent/enttest"
	"eino-quickstart/internal/platform/privacy"

	"entgo.io/ent/dialect"
	_ "github.com/mattn/go-sqlite3"
)

func TestNewStorePreservesDependencies(t *testing.T) {
	policy, err := privacy.NewArgumentPolicy(1024, []string{"token"})
	if err != nil {
		t.Fatal(err)
	}

	store := NewStore(nil, policy, time.Minute)
	if store.argumentPolicy.MaxBytes != 1024 {
		t.Fatalf("max bytes = %d, want 1024", store.argumentPolicy.MaxBytes)
	}
	if store.approvalTTL != time.Minute {
		t.Fatalf("approval TTL = %s, want %s", store.approvalTTL, time.Minute)
	}
}

func TestApprovalLifecycleIsSingleUseAndAudited(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, dialect.SQLite, "file:approval-lifecycle?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	policy, err := privacy.NewArgumentPolicy(4096, []string{"token"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(client, policy, time.Hour)
	args := `{"path":"/tmp/p05-safe.txt","token":"secret"}`
	id, err := store.Create(ctx, "session-1", "turn-1", "agent-a", "write_file", args)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AttachCheckpoint(ctx, id, "run-1", "checkpoint-1", "interrupt-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.Decide(ctx, id, true, "approver"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimForResume(ctx, id, "agent-a"); err != nil {
		t.Fatal(err)
	}
	if err := store.Consume(ctx, id, "session-1", "agent-a", "write_file", args); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimForResume(ctx, id, "agent-a"); err == nil {
		t.Fatal("executed approval must not be resumable twice")
	}
	if err := store.Consume(ctx, id, "session-1", "agent-a", "write_file", args); err == nil {
		t.Fatal("executed approval must not be consumable twice")
	}
	record, ok := store.Get(ctx, id)
	if !ok || record.Status != StatusExecuted {
		t.Fatalf("final status=%+v ok=%v, want EXECUTED", record, ok)
	}
	count, err := client.AuditEvent.Query().Count(ctx)
	if err != nil || count < 4 {
		t.Fatalf("audit count=%d err=%v, want create/approve/resume/execute", count, err)
	}
	if record.DisplayArguments == nil || strings.Contains(*record.DisplayArguments, "secret") {
		t.Fatalf("approval display arguments leaked sensitive token: %v", record.DisplayArguments)
	}
}
