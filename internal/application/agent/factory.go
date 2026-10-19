package agent

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/internal/application/contextmgr"
	"eino-quickstart/internal/application/middleware"
	"eino-quickstart/internal/application/providermodel"
	"eino-quickstart/internal/platform/config"
	"eino-quickstart/internal/platform/secretbox"
	"eino-quickstart/internal/tool/registry"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
)

type Harness struct {
	Runner *adk.Runner

	// GeneralRunner 专门跑兜底专员，与 Runner 共享同一个 general_agent 实例。
	// 主 Runner 一轮没有任何产出时，ChatLogic 用它补跑一次。
	//
	// 它故意不挂 CheckPointStore：兜底专员没有工具，不可能产生审批中断，不需要
	// 断点续跑；而复用主 store 会让同一个 runID 的断点被两套执行覆盖。
	GeneralRunner *adk.Runner

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

// newSpecialist 用统一配置里的模型构造一个专员。
//
// maxIterations <= 0 表示沿用 cfg.Agent.MaxIterations（三个专项专员都是这样）；
// root 与 general 各自传固定上限，因为它们要表达的循环次数是确定的。
func newSpecialist(ctx context.Context, cfg *config.Config, name string, description string, instruction string, tools []tool.BaseTool, handlers []adk.ChatModelAgentMiddleware, maxIterations int,
) (adk.Agent, error) {
	chatModel, err := newChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create model for %s: %w", name, err)
	}

	return newSpecialistWithModel(ctx, cfg, name, description, instruction, tools, handlers, chatModel, maxIterations)
}

// newSpecialistWithModel 是唯一真正构造 agent 的地方。
//
// 把「选模型」与「装 agent」拆开，是为了让兜底专员能注入一个来自数据库配置的
// 外部平台模型（见 providermodel），而不必复制一整套装配代码。
func newSpecialistWithModel(ctx context.Context, cfg *config.Config, name string, description string, instruction string, tools []tool.BaseTool, handlers []adk.ChatModelAgentMiddleware, chatModel model.BaseChatModel, maxIterations int,
) (adk.Agent, error) {
	if maxIterations <= 0 {
		maxIterations = cfg.Agent.MaxIterations
	}

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          name,
		Description:   description,
		Instruction:   instruction,
		Model:         chatModel,
		MaxIterations: maxIterations,
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

func NewHarness(ctx context.Context, cfg *config.Config, registry *registry.Registry, policy *middleware.Policy, checkpoints adk.CheckPointStore, entClient *ent.Client, box *secretbox.Box) (*Harness, error) {
	knowledgeTools, err := registry.Require("search_knowledge")
	if err != nil {
		return nil, err
	}

	// Skills are read-only guidance for the workspace specialist. Grant them here
	// (rather than globally) so loading a Skill never expands write/execute powers.
	workspaceTools, err := registry.Require("read_file", "list_dir", "list_skills", "load_skill")
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

	generalChatModel, err := resolveGeneralChatModel(ctx, cfg, entClient, box)
	if err != nil {
		return nil, err
	}

	generalAgent, err := newGeneralAgent(ctx, cfg, generalChatModel)
	if err != nil {
		return nil, err
	}

	rootAgent, err := newRootAgent(ctx, cfg, knowledgeAgent, workspaceAgent, automationAgent, generalAgent, []adk.ChatModelAgentMiddleware{contextLimit})
	if err != nil {
		return nil, err
	}

	return &Harness{
		Runner: adk.NewRunner(ctx, adk.RunnerConfig{
			Agent:           rootAgent,
			EnableStreaming: true,
			CheckPointStore: checkpoints,
		}),
		GeneralRunner: adk.NewRunner(ctx, adk.RunnerConfig{
			Agent:           generalAgent,
			EnableStreaming: true,
		}),
		Context: contextmgr.New(
			cfg.Context.MaxHistoryMessages,
			cfg.Context.MaxToolOutputBytes,
		),
	}, nil
}

// resolveGeneralChatModel 决定兜底专员用哪个模型。
//
// 优先用数据库里配置的外部平台（ModelRouting.default_chat_model → Model →
// ModelProvider）。两种情况回落 config.yaml 的 Model 段：
//
//  1. 没配路由 —— 初次部署的正常状态，不该拦住启动；
//  2. 没有主密钥 —— 数据库里的密文解不开，说清楚不如直接跟随统一配置。
//
// 但「配了 key 却解析不出来」是另一回事：provider 被删、密文损坏、模型名打错，
// 这些是部署错误。此时静默回落只会让「我明明配了外部平台，回答却还是本地模型」
// 这种问题拖很久，所以直接返回 error 让启动失败。
func resolveGeneralChatModel(
	ctx context.Context,
	cfg *config.Config,
	entClient *ent.Client,
	box *secretbox.Box,
) (model.BaseChatModel, error) {
	if entClient == nil || box == nil {
		return newChatModel(ctx, cfg)
	}

	resolver := providermodel.NewResolver(entClient, box)

	key, err := resolver.DefaultChatModelKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve general model routing: %w", err)
	}
	if key == "" {
		return newChatModel(ctx, cfg)
	}

	// 先解析一次：既是启动期校验（配置错了现在就炸），也顺手把缓存捂热，
	// 让第一条兜底请求不必多打几次数据库。
	if _, err := resolver.ChatModel(ctx, key); err != nil {
		return nil, fmt.Errorf("resolve general model %q: %w", key, err)
	}

	return providermodel.NewDynamic(resolver, key), nil
}
