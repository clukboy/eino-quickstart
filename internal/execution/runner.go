package execution

import (
	"context"
	"errors"
)

type Command struct {
	Workspace string
	Script    string
}

type Result struct {
	Output    string
	Truncated bool
}
type Runner interface {
	Run(ctx context.Context, cmd Command) (Result, error)
}

var ErrExecutionDisabled = errors.New("command execution is disabled")

type DisabledRunner struct{}

func NewDisabledRunner() *DisabledRunner {
	return &DisabledRunner{}
}

func (d *DisabledRunner) Run(ctx context.Context, cmd Command) (Result, error) {
	return Result{}, ErrExecutionDisabled
}
