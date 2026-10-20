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
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// knowledgeToolName 是这条链路上唯一的工具。
const knowledgeToolName = "search_knowledge"

// Harness 是「模型 + 工具 + 上下文策略」的装配结果，本身不跑业务规则。
//
// 它只有两个出口：
//   - Runner：给定 agentic 消息序列，产出事件流（谁去消费、怎么落库不关它的事）；
//   - Context：历史裁剪与工具输出截断的阈值（一轮对话怎么组织由 chat.Service 决定）。
//
// 一轮对话的编排在 internal/application/chat —— 这里刻意不引 persistence，
// 否则「跑一轮」和「存一轮」又会粘在一起。
type Harness struct {
	Runner  *adk.TypedRunner[*schema.AgenticMessage]
	Context *contextmgr.Manager
}

func NewHarness(ctx context.Context, cfg *config.Config, toolRegistry *registry.Registry) (*Harness, error) {
	if cfg.Model.APIKey == "" {
		return nil, fmt.Errorf("model.api_key is empty; set EINO_MODEL_API_KEY")
	}

	// Responses API（agentic 体系）：消息是 content blocks，runner 是泛型
	// TypedRunner[*schema.AgenticMessage]。换回 Chat Completions 时整条链路的
	// 事件类型都要跟着换，不是只改这一行。
	am, err := agenticopenai.NewResponsesModel(ctx, &agenticopenai.ResponsesConfig{
		APIKey:          cfg.Model.APIKey,
		Model:           cfg.Model.Model,
		BaseURL:         cfg.Model.BaseURL,
		Temperature:     &cfg.Model.Temperature,
		EnableAutoCache: true,
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

	return &Harness{
		Runner: adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{
			Agent:           chatAgent,
			EnableStreaming: true,
		}),
		Context: contextmgr.New(
			cfg.Context.MaxHistoryMessages,
			cfg.Context.MaxToolOutputBytes,
		),
	}, nil
}

// UserMessage 让应用层不必依赖 HTTP 层的消息类型。
//
// 返回值必须与 Runner 的元素类型一致（AgenticMessage）：这一轮的输入要和
// History 从库里读出来的历史排在同一个切片里，类型一旦不同就拼不起来。
func UserMessage(content string) *schema.AgenticMessage { return schema.UserAgenticMessage(content) }
