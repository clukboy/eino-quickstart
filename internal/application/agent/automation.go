package agent

import (
	"context"
	"eino-quickstart/internal/platform/config"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
)

const automationInstruction = `
You are the controlled automation specialist.

Rules:
- Use tools only when they are necessary to complete a requested workspace operation.
- Describe the intended operation before invoking a write or command tool.
- Respect all tool-policy, approval, and workspace-boundary errors.
- Do not retry an approval-required operation with altered arguments.
- Do not answer knowledge-base questions unless the information is available from tool output.
- Report actual tool output; never claim success without a successful tool result.
`

func newAutomationAgent(
	ctx context.Context,
	cfg *config.Config,
	tools []tool.BaseTool,
	handlers []adk.ChatModelAgentMiddleware,
) (adk.Agent, error) {
	return newSpecialist(
		ctx,
		cfg,
		"automation_agent",
		"Performs controlled workspace changes and commands under the harness approval policy.",
		automationInstruction,
		tools,
		handlers,
	)
}
