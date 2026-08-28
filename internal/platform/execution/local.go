package execution

import (
	"context"
	"fmt"
	"os/exec"
)

type LocalRunner struct {
	maxOutput int
}

func NewLocalRunner(maxOutput int) (*LocalRunner, error) {
	if maxOutput <= 0 {
		return nil, fmt.Errorf(
			"maximum output must be greater than zero",
		)
	}

	return &LocalRunner{
		maxOutput: maxOutput,
	}, nil
}

func (r *LocalRunner) Run(
	ctx context.Context,
	command Command,
) (Result, error) {
	cmd := exec.CommandContext(
		ctx,
		"/bin/sh",
		"-lc",
		command.Script,
	)
	cmd.Dir = command.Workspace

	var output limitedBuffer
	output.limit = r.maxOutput

	cmd.Stdout = &output
	cmd.Stderr = &output

	err := cmd.Run()

	result := Result{
		Output:    output.String(),
		Truncated: output.truncated,
	}

	if output.truncated {
		result.Output += "\n...[command output truncated by harness]"
	}

	if ctx.Err() != nil {
		return result, fmt.Errorf(
			"local command timed out: %w",
			ctx.Err(),
		)
	}

	return result, err
}
