package agent

import (
	"context"
	"fmt"

	"eino-quickstart/internal/application/contextmgr"
	"eino-quickstart/internal/application/middleware"
	"eino-quickstart/internal/platform/config"
	"eino-quickstart/internal/tool/registry"

	"github.com/cloudwego/eino-ext/components/model/agenticopenai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
)

// knowledgeToolName 是这条链路上唯一的工具。
const knowledgeToolName = "search_knowledge"

type Harness struct {
	Runner  *adk.TypedRunner[*schema.AgenticMessage]
	Context *contextmgr.Manager

	ModelOptions []model.Option
}

func NewHarness(ctx context.Context, cfg *config.Config, toolRegistry *registry.Registry) (*Harness, error) {
	if cfg.Model.APIKey == "" {
		return nil, fmt.Errorf("model.api_key is empty; set EINO_MODEL_API_KEY")
	}

	// Responses API（agentic 体系）：消息是 content blocks，runner 是泛型
	// TypedRunner[*schema.AgenticMessage]。换回 Chat Completions 时整条链路的
	// 事件类型都要跟着换，不是只改这一行。
	serverTools, include := serverToolOptions(cfg.Model.ServerTools)

	am, err := agenticopenai.NewResponsesModel(ctx, &agenticopenai.ResponsesConfig{
		APIKey:          cfg.Model.APIKey,
		Model:           cfg.Model.Model,
		BaseURL:         cfg.Model.BaseURL,
		Temperature:     &cfg.Model.Temperature,
		EnableAutoCache: true,
		Include:         include,
	})
	if err != nil {
		return nil, fmt.Errorf("create responses model: %w", err)
	}
	tools, err := toolRegistry.Require(knowledgeToolName)
	if err != nil {
		return nil, err
	}

	handlers := []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]{
		middleware.NewToolOutputLimit(cfg.Context.MaxToolOutputBytes),
	}

	chatAgent, err := adk.NewTypedChatModelAgent(ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
		Name:          cfg.Agent.Name,
		Description:   "Answers questions from the organization knowledge base and handles general conversation.",
		Instruction:   cfg.Agent.Instruction,
		Model:         am,
		MaxIterations: cfg.Agent.MaxIterations,
		Handlers:      handlers,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: tools,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}

	harness := &Harness{
		Runner: adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{
			Agent:           chatAgent,
			EnableStreaming: true,
		}),
		Context: contextmgr.New(
			cfg.Context.MaxHistoryMessages,
			cfg.Context.MaxToolOutputBytes,
		),
	}

	if len(serverTools) > 0 {
		harness.ModelOptions = []model.Option{
			agenticopenai.WithResponsesServerTools(serverTools),
		}
	}

	return harness, nil
}

func serverToolOptions(names []string) (tools []*agenticopenai.ResponsesServerToolConfig, include []responses.ResponseIncludable) {
	for _, name := range names {
		switch name {
		case config.ServerToolWebSearch:
			tools = append(tools, &agenticopenai.ResponsesServerToolConfig{
				WebSearch: &responses.WebSearchToolParam{
					Type: responses.WebSearchToolTypeWebSearch,
				},
			})
			include = append(include, responses.ResponseIncludableWebSearchCallActionSources)

		case config.ServerToolCodeInterpreter:
			tools = append(tools, &agenticopenai.ResponsesServerToolConfig{
				CodeInterpreter: &responses.ToolCodeInterpreterParam{
					// container 是必填项：不指定容器时传 "auto"，由服务端临时起一个。
					Container: responses.ToolCodeInterpreterContainerUnionParam{
						OfString: param.NewOpt("auto"),
					},
				},
			})

		case config.ServerToolShell:
			tools = append(tools, &agenticopenai.ResponsesServerToolConfig{
				Shell: &responses.FunctionShellToolParam{},
			})

		default:
			return nil, nil
		}
	}

	return tools, include
}

func UserMessage(content string) *schema.AgenticMessage { return schema.UserAgenticMessage(content) }
