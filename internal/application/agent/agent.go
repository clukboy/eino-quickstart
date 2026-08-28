package agent

import (
	"context"
	"eino-quickstart/internal/application/contextmgr"
	"eino-quickstart/internal/application/middleware"
	"eino-quickstart/internal/platform/config"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	schema "github.com/cloudwego/eino/schema"
)

type Agent struct {
	Runner  *adk.Runner
	Context *contextmgr.Manager
}

func New(ctx context.Context, cfg *config.Config, tools []tool.BaseTool, policy *middleware.Policy) (*Agent, error) {
	if cfg.Model.APIKey == "" {
		return nil, fmt.Errorf("model.api_key is empty; set EINO_MODEL_API_KEY")
	}
	cm, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:      cfg.Model.APIKey,
		Model:       cfg.Model.Model,
		BaseURL:     cfg.Model.BaseURL,
		Temperature: &cfg.Model.Temperature,
	})
	if err != nil {
		return nil, fmt.Errorf("create chat model failed: %w", err)
	}

	var handlers []adk.ChatModelAgentMiddleware
	if policy != nil {
		handlers = append(handlers, policy)
	}
	handlers = append(handlers, middleware.NewToolOutputLimit(cfg.Context.MaxToolOutputBytes))
	cmgr := contextmgr.New(cfg.Context.MaxHistoryMessages, cfg.Context.MaxToolOutputBytes)
	instruction := cmgr.BuildInstruction(cfg.Agent.Instruction, cfg.Workspace.Root)

	a, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{Name: cfg.Agent.Name, Description: "Eino Harness agent with skills, context controls and tool policy.", Instruction: instruction, Model: cm, MaxIterations: cfg.Agent.MaxIterations, Handlers: handlers, ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools}}})
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}
	return &Agent{Runner: adk.NewRunner(ctx, adk.RunnerConfig{Agent: a, EnableStreaming: true, CheckPointStore: nil}), Context: cmgr}, nil
}

// UserMessage Message is kept here to make the application layer independent of the HTTP layer.
func UserMessage(content string) *schema.Message { return schema.UserMessage(content) }
