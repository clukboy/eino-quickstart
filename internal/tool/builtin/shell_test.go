package builtin

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestShellLimitsOutput(t *testing.T) {
	shell := &Shell{Root: t.TempDir(), Timeout: time.Second, MaxOutputBytes: 4}
	output, err := shell.run(context.Background(), shellInput{Command: "printf 123456"})
	if err != nil {
		t.Fatal(err)
	}
	if output != "1234\n...[command output truncated by harness]" {
		t.Fatalf("output = %q", output)
	}
}

func TestShellStopsAtDeadline(t *testing.T) {
	shell := &Shell{Root: t.TempDir(), Timeout: 10 * time.Millisecond, MaxOutputBytes: 1024}
	_, err := shell.run(context.Background(), shellInput{Command: "sleep 1"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
}
