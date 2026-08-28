package execution

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
)

type DockerConfig struct {
	Binary       string
	Image        string
	User         string
	MemoryLimit  string
	CPULimit     string
	PIDsLimit    int
	TmpFSSize    string
	AllowNetwork bool
	MaxOutput    int
}

type DockerRunner struct {
	config DockerConfig
}

func NewDockerRunner(config DockerConfig) (*DockerRunner, error) {
	if config.Binary == "" {
		config.Binary = "docker"
	}
	binary, err := exec.LookPath(config.Binary)
	if err != nil {
		return nil, fmt.Errorf("find docker binary:%w", err)
	}
	if config.Image == "" {
		return nil, fmt.Errorf("docker image is required")
	}
	if config.User == "" {
		return nil, fmt.Errorf("docker user is required")
	}
	if config.MemoryLimit == "" {
		return nil, fmt.Errorf("docker memory limit is required")
	}
	if config.CPULimit == "" {
		return nil, fmt.Errorf("docker cpu limit is required")
	}
	if config.PIDsLimit <= 0 {
		return nil, fmt.Errorf("docker pids limit must be greater than zero")
	}
	if config.MaxOutput <= 0 {
		return nil, fmt.Errorf("docker max output must be greater than zero")
	}
	config.Binary = binary
	return &DockerRunner{config: config}, nil
}

func (r *DockerRunner) Run(ctx context.Context, command Command) (Result, error) {
	workspace, err := filepath.Abs(command.Workspace)
	if err != nil {
		return Result{}, fmt.Errorf("failed to get absolute path of workspace: %w", err)
	}
	args := []string{
		"run",
		"--rm",
		"--init",
		"--read-only",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--pids-limit", fmt.Sprintf("%d", r.config.PIDsLimit),
		"--memory", r.config.MemoryLimit,
		"--cpus", r.config.CPULimit,
		"--user", r.config.User,
		"--workdir", "/workspace",
		"--mount",
		fmt.Sprintf(
			"type=bind,src=%s,dst=/workspace,rw",
			workspace,
		),
		"--tmpfs",
		fmt.Sprintf(
			"/tmp:rw,noexec,nosuid,size=%s",
			r.config.TmpFSSize,
		),
	}
	if !r.config.AllowNetwork {
		args = append(args, "--network", "none")
	}
	args = append(args, r.config.Image, "/bin/sh", "-lc", command.Script)

	cmd := exec.CommandContext(ctx, r.config.Binary, args...)

	var output limitedBuffer
	output.limit = r.config.MaxOutput
	cmd.Stdout = &output
	cmd.Stderr = &output

	err = cmd.Run()
	result := Result{
		Output:    output.String(),
		Truncated: output.truncated,
	}
	if output.truncated {
		result.Output += "\n...[command output truncated by harness]"
	}
	if ctx.Err() != nil {
		return result, fmt.Errorf("container command timed out: %w", ctx.Err())
	}
	return result, err
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	if b.buffer.Len() >= b.limit {
		b.truncated = true
		return len(data), nil
	}

	remaining := b.limit - b.buffer.Len()
	if len(data) > remaining {
		_, _ = b.buffer.Write(data[:remaining])
		b.truncated = true
		return len(data), nil
	}

	return b.buffer.Write(data)
}

func (b *limitedBuffer) String() string {
	return b.buffer.String()
}
