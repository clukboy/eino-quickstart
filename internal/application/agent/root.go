package agent

import (
	"context"
	"eino-quickstart/internal/platform/config"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
)

const rootInstruction = `
You are the root coordinator.

Delegate work to exactly one specialist whenever possible:
- knowledge_agent: questions requiring knowledge-base facts and citations.
- workspace_agent: reading or inspecting files and directories.
- automation_agent: writing files or executing approved workspace operations.

Rules:
- Do not directly answer facts that should come from the knowledge base.
- Do not directly inspect, write, or execute workspace operations.
- Do not invent specialist results.
- Preserve citations returned by knowledge_agent.
- If automation requires approval, return the approval requirement exactly as reported.
- Do not delegate the same request repeatedly after a specialist has returned a clear result.
`

func newRootAgent(
	ctx context.Context,
	cfg *config.Config,
	knowledge adk.Agent,
	workspace adk.Agent,
	automation adk.Agent,
	handlers []adk.ChatModelAgentMiddleware,
) (adk.Agent, error) {
	tools := []tool.BaseTool{
		adk.NewAgentTool(ctx, knowledge),
		adk.NewAgentTool(ctx, workspace),
		adk.NewAgentTool(ctx, automation),
	}

	return newSpecialist(
		ctx,
		cfg,
		"root_agent",
		"Routes requests to knowledge, workspace inspection, or controlled automation specialists.",
		rootInstruction,
		tools,
		handlers,
	)
}
