package agent

import (
	"context"
	"eino-quickstart/internal/config"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
)

const workspaceInstruction = `
You are the workspace inspection specialist.

Rules:
- Use read_file and list_dir only.
- Never write, delete, rename, or execute anything.
- Never infer file content that was not read.
- Summarize findings with file paths.
- If a requested path is inaccessible, report the tool error.
`

func newWorkspaceAgent(
	ctx context.Context,
	cfg *config.Config,
	tools []tool.BaseTool,
	handlers []adk.ChatModelAgentMiddleware,
) (adk.Agent, error) {
	return newSpecialist(
		ctx,
		cfg,
		"workspace_agent",
		"Inspects files and directories in the workspace without making changes.",
		workspaceInstruction,
		tools,
		handlers,
	)
}
