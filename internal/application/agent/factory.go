package agent

import (
	"context"
	"eino-quickstart/internal/application/contextmgr"
	"eino-quickstart/internal/application/middleware"
	"eino-quickstart/internal/platform/config"
	"eino-quickstart/internal/tool/registry"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
)

type Harness struct {
	Runner  *adk.Runner
	Context *contextmgr.Manager
}

func newChatModel(
	ctx context.Context,
	cfg *config.Config,
) (*openai.ChatModel, error) {
	return openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:      cfg.Model.APIKey,
		Model:       cfg.Model.Model,
		BaseURL:     cfg.Model.BaseURL,
		Temperature: &cfg.Model.Temperature,
	})
}

func newSpecialist(
	ctx context.Context,
	cfg *config.Config,
	name string,
	description string,
	instruction string,
	tools []tool.BaseTool,
	handlers []adk.ChatModelAgentMiddleware,
) (adk.Agent, error) {
	model, err := newChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create model for %s: %w", name, err)
	}

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          name,
		Description:   description,
		Instruction:   instruction,
		Model:         model,
		MaxIterations: cfg.Agent.MaxIterations,
		Handlers:      handlers,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: tools,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", name, err)
	}

	return agent, nil
}

func NewHarness(
	ctx context.Context,
	cfg *config.Config,
	registry *registry.Registry,
	policy *middleware.Policy,
	checkpoints adk.CheckPointStore,
) (*Harness, error) {
	knowledgeTools, err := registry.Require("search_knowledge")
	if err != nil {
		return nil, err
	}

	workspaceTools, err := registry.Require("read_file", "list_dir")
	if err != nil {
		return nil, err
	}

	automationToolNames := []string{"write_file"}
	if cfg.Execution.Mode != "disabled" {
		automationToolNames = append(automationToolNames, "shell")
	}

	automationTools, err := registry.Require(automationToolNames...)
	if err != nil {
		return nil, err
	}

	contextLimit := middleware.NewToolOutputLimit(
		cfg.Context.MaxToolOutputBytes,
	)

	knowledgeAgent, err := newKnowledgeAgent(
		ctx,
		cfg,
		knowledgeTools,
		[]adk.ChatModelAgentMiddleware{contextLimit},
	)
	if err != nil {
		return nil, err
	}

	workspaceAgent, err := newWorkspaceAgent(
		ctx,
		cfg,
		workspaceTools,
		[]adk.ChatModelAgentMiddleware{contextLimit},
	)
	if err != nil {
		return nil, err
	}

	automationHandlers := []adk.ChatModelAgentMiddleware{
		contextLimit,
	}
	if policy != nil {
		automationHandlers = append(
			[]adk.ChatModelAgentMiddleware{policy},
			automationHandlers...,
		)
	}

	automationAgent, err := newAutomationAgent(
		ctx,
		cfg,
		automationTools,
		automationHandlers,
	)
	if err != nil {
		return nil, err
	}

	rootAgent, err := newRootAgent(ctx, cfg, knowledgeAgent, workspaceAgent, automationAgent, []adk.ChatModelAgentMiddleware{contextLimit})
	if err != nil {
		return nil, err
	}

	return &Harness{
		Runner: adk.NewRunner(ctx, adk.RunnerConfig{
			Agent:           rootAgent,
			EnableStreaming: true,
			CheckPointStore: checkpoints,
		}),
		Context: contextmgr.New(
			cfg.Context.MaxHistoryMessages,
			cfg.Context.MaxToolOutputBytes,
		),
	}, nil
}
