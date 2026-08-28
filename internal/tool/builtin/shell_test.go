package builtin

import (
	"context"
	"eino-quickstart/internal/platform/execution"
	"errors"
	"testing"
	"time"
)

type stubRunner struct {
	run func(context.Context, execution.Command) (execution.Result, error)
}

func (s stubRunner) Run(
	ctx context.Context,
	command execution.Command,
) (execution.Result, error) {
	return s.run(ctx, command)
}

func TestShellDelegatesToRunner(t *testing.T) {
	shell := &Shell{
		Root:    t.TempDir(),
		Timeout: time.Second,
		Runner: stubRunner{
			run: func(
				_ context.Context,
				command execution.Command,
			) (execution.Result, error) {
				if command.Script != "printf 123456" {
					t.Fatalf("command = %q", command.Script)
				}
				return execution.Result{Output: "1234"}, nil
			},
		},
	}
	output, err := shell.run(context.Background(), shellInput{Command: "printf 123456"})
	if err != nil {
		t.Fatal(err)
	}
	if output != "1234" {
		t.Fatalf("output = %q", output)
	}
}

func TestShellStopsAtDeadline(t *testing.T) {
	shell := &Shell{
		Root:    t.TempDir(),
		Timeout: 10 * time.Millisecond,
		Runner: stubRunner{
			run: func(ctx context.Context, _ execution.Command) (execution.Result, error) {
				<-ctx.Done()
				return execution.Result{}, ctx.Err()
			},
		},
	}
	_, err := shell.run(context.Background(), shellInput{Command: "sleep 1"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
}
