package approval

import "testing"

func TestStoreDecideOnlyOnce(t *testing.T) {
	store := NewStore()
	record := store.Create("session-1", "shell", `{"command":"go test ./..."}`)

	if err := store.Decide(record.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := store.Decide(record.ID, false); err == nil {
		t.Fatal("expected duplicate decision to fail")
	}
}

func TestConsumeExecuteApprovalOnlyOnce(t *testing.T) {
	store := NewStore()
	record := store.Create("session-1", "shell", `{"command":"go test ./..."}`)

	if err := store.Decide(record.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := store.Consume(record.ID, "session-1", "shell", `{"command":"go test ./..."}`); err != nil {
		t.Fatal(err)
	}
	if err := store.Consume(record.ID, "session-1", "shell", `{"command":"go test ./..."}`); err == nil {
		t.Fatal("expected duplicate consume to fail")
	}
}

func TestConsumeRejectsChangedArguments(t *testing.T) {
	store := NewStore()
	record := store.Create(
		"session-1",
		"shell",
		`{"command":"go test ./..."}`,
	)

	if err := store.Decide(record.ID, true); err != nil {
		t.Fatal(err)
	}

	err := store.Consume(
		record.ID,
		"session-1",
		"shell",
		`{"command":"rm -rf ."}`,
	)
	if err == nil {
		t.Fatal("changed arguments should be rejected")
	}
}
