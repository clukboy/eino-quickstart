package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReportFinalStatusPrioritizesFailure(t *testing.T) {
	r := &report{}
	r.add(statusBlocked, "redis", "Redis unavailable", "connection refused")
	r.add(statusFail, "milvus", "dimension mismatch", "configured=1024 actual=1536")
	status, _ := r.finalStatus()
	if status != statusFail {
		t.Fatalf("finalStatus() = %q, want %q", status, statusFail)
	}
}

func TestReportFinalStatusBlocksWithoutFailing(t *testing.T) {
	r := &report{}
	r.add(statusPass, "config", "config loaded", "")
	r.add(statusBlocked, "worker", "worker not registered", "")
	status, message := r.finalStatus()
	if status != statusBlocked {
		t.Fatalf("finalStatus() = %q, want %q", status, statusBlocked)
	}
	if message == "" {
		t.Fatal("finalStatus() returned an empty remediation message")
	}
}

func TestWriteFixtureUsesUniqueMarkerAndRestrictedPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.md")
	if err := writeFixture(path, "20261007T010203Z"); err != nil {
		t.Fatalf("writeFixture() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), "eino-acceptance-p02-20261007T010203Z") {
		t.Fatalf("fixture does not contain the run marker: %s", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("fixture permissions = %o, want 600", got)
	}
}

func TestFinishWritesRedactedJSONReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preflight.json")
	r := &report{
		Plan:      "P02",
		Mode:      "degraded",
		RunID:     "run-1",
		Config:    map[string]string{"api": "configs/acceptance/api.yaml"},
		Resources: map[string]string{"database": "127.0.0.1:5432/eino_acceptance"},
		Checks:    []check{{Name: "config", Status: statusPass, Summary: "ok"}},
	}
	finish(r, statusPass, "ok", path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var got report
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("report is not JSON: %v", err)
	}
	if got.Status != statusPass || got.FinishedAt.IsZero() {
		t.Fatalf("report status/time = %q/%v", got.Status, got.FinishedAt)
	}
	if strings.Contains(string(data), "password") || strings.Contains(string(data), "secret") {
		t.Fatalf("report contains a secret-like field: %s", data)
	}
}
