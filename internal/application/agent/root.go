package agent

import (
	"context"
	"eino-quickstart/internal/platform/config"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
)

// rootMaxIterations 是 3，不是配置里的 20。
//
// MaxIterations 的检查发生在每次模型调用之前（eino/adk/react.go:387），所以 3
// 允许三次调用：① 发工具调用选人、② 拿到专员结果后作答、③ 一次容错。router 跑偏
// 时会连续调用多个专员，每次都是一轮完整的子 agent 执行 —— 沿用 20 会让一次跑偏
// 变成一个又慢又难定位的长请求。
const rootMaxIterations = 3

// rootInstruction 把 root 定义成一个**只选人、不答题**的路由器。
//
// 这是本次改造的核心决定。让 root 保留「自己答」的能力看起来很省事（闲聊只花
// 一次模型调用），但那样「不匹配任何专员时默认走兜底」就只是提示词偏好，模型
// 随时可以自己答，语义无法保证；而且 general_agent 那套「不能编造实时数据」的
// 约束根本不会生效 —— root 的 instruction 里没有这些规则。
//
// 代价是闲聊从 1 次模型调用变成 3 次（路由 / 作答 / 复述）。这个代价是知情的：
// 要的是路由结果的确定性与可观测性（每条请求都有 routed_to），不是省一次调用。
const rootInstruction = `
You are the router of a multi-agent assistant. You never answer the user
yourself: every request must be handed to exactly one specialist through a
tool call.

Specialists:
- knowledge_agent: the answer must come from the organization's own documents
  (product specs, internal policies, handbooks, uploaded datasets) and should
  carry citations. A question merely sounding factual is NOT a reason to use it.
- workspace_agent: the user asks what a specific file or directory in the
  workspace contains.
- automation_agent: the user asks to create or modify files, or to run commands.
- general_agent: the default. Everything else: greetings and small talk, general
  world knowledge, explanations, translation, writing, coding help, arithmetic,
  planning, opinions, live data it cannot fetch, and any request you are unsure
  about.

Routing rules:
1. Call exactly one tool per user request. Do not answer with prose instead of a
   tool call, and do not write prose in the same turn as the tool call.
2. Prefer general_agent whenever the request does not clearly require one of the
   other three. When in doubt, choose general_agent.
3. Never call knowledge_agent unless the answer has to come from the
   organization's documents. Never call workspace_agent or automation_agent
   unless the request is about workspace files or file or command operations.
4. Pass the user's request in the request argument, in the user's own words and
   language. Specialists do not see the conversation history, so if the message
   is a follow-up, restate the missing context inside the request.
5. After the specialist returns, reply with its answer. Preserve citations,
   approval requirements, and any statement that it cannot access realtime data.
   Do not add facts the specialist did not provide.
6. Do not call a second specialist for the same request.
7. Do not repeat the request that has already been answered.
`

func newRootAgent(ctx context.Context, cfg *config.Config, knowledge adk.Agent, workspace adk.Agent, automation adk.Agent, general adk.Agent, handlers []adk.ChatModelAgentMiddleware) (adk.Agent, error) {
	tools := []tool.BaseTool{
		adk.NewAgentTool(ctx, knowledge),
		adk.NewAgentTool(ctx, workspace),
		adk.NewAgentTool(ctx, automation),
		adk.NewAgentTool(ctx, general),
	}

	return newSpecialist(
		ctx,
		cfg,
		"root_agent",
		"Routes every request to exactly one specialist: knowledge, workspace inspection, controlled automation, or the general fallback. Never answers by itself.",
		rootInstruction,
		tools,
		handlers,
		rootMaxIterations,
	)
}
