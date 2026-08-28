package builtin

import (
	"context"
	"eino-quickstart/internal/execution"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

type Shell struct {
	Root    string
	Timeout time.Duration
	Runner  execution.Runner
}
type shellInput struct {
	Command string `json:"command" jsonschema_description:"Shell command to execute in the workspace"`
}

func NewShell(root string, timeout time.Duration, runner execution.Runner) (tool.BaseTool, error) {
	if runner == nil {
		return nil, fmt.Errorf("execution runner is required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root symlinks: %w", err)
	}
	info, err := os.Stat(resolvedRoot)
	if err != nil {
		return nil, fmt.Errorf("stat workspace root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace root is not a directory: %s", root)
	}
	s := &Shell{Root: resolvedRoot, Timeout: timeout, Runner: runner}
	return utils.InferTool("shell", "Execute a shell command in the workspace. Use it for build, text, git, and other development commands.", s.run)
}

func (s *Shell) run(
	ctx context.Context,
	in shellInput,
) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	if strings.TrimSpace(in.Command) == "" {
		return "", fmt.Errorf("command is required")
	}

	result, err := s.Runner.Run(ctx, execution.Command{
		Workspace: s.Root,
		Script:    in.Command,
	})

	return result.Output, err
}
