package agent

import (
	"context"

	"eino-quickstart/internal/platform/config"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
)

// GeneralAgentName 同时是三样东西：agent 名、root 侧的工具名、SSE 帧里的 agent 字段。
// 导出它是为了让 transport 层不必再写一遍字符串字面量。
const GeneralAgentName = "general_agent"

// generalMaxIterations 是 1，因为无工具 agent 只有一次模型调用，ReAct 的迭代上限
// 根本不会被用到（buildRunFunc 走 no-tools 分支）。写出来是为了表达「就一次」。
const generalMaxIterations = 1

// generalDescription 是 root 挑人时唯一能看到的说明，所以必须同时写清三件事：
// 覆盖面、排他条件、默认语义。排在最后的 "whenever you are unsure" 是关键 ——
// 兜底能不能兜住，取决于模型在拿不准时会不会选它。
const generalDescription = "Default specialist for every request that does not clearly require the knowledge base, " +
	"workspace inspection, or file automation: ordinary conversation, general knowledge, explanations, " +
	"translation, writing, summarising, brainstorming, coding help, arithmetic, planning, and requests " +
	"for live data it cannot fetch. Use this whenever no other specialist clearly applies, and whenever " +
	"you are unsure which specialist to pick. It has no tools."

// generalInstruction 的核心是「不许假装查过」。
//
// 兜底专员的模型可能来自外部平台（见 providermodel），也可能是本地主模型；无论哪
// 种它都没有工具，因此任何「我帮你查了」都是编造。这里把最常见、最容易被编造的
// 实时数据类别逐条点名出来，比写一句抽象的 "no realtime data" 有效得多。
//
// 注意：本段不能出现花括号。root 调子 agent 时会带 session values，Eino 的
// defaultGenModelInput 会用 prompt.FString 格式化 instruction，花括号会被当成占位符。
const generalInstruction = `
You are the general specialist and the default responder of this assistant.
You have no tools. Everything you say must come from your own knowledge and from
the conversation so far.

Rules:
- Handle ordinary conversation, general world knowledge, explanations, writing,
  translation, summarising, brainstorming, coding help, arithmetic and planning.
- Never claim to have browsed, searched, read a file, called an API, or checked
  anything live. You cannot access realtime or post-training-cutoff information:
  weather, prices, exchange rates, news, sports results, traffic, schedules,
  stock quotes, or anyone's current status.
- When a request depends on such live data, say plainly that you cannot fetch
  realtime data. Then give the stable part you do know and ask what you need.
- Never invent numbers, dates, names, quotations, URLs, citations or "latest"
  claims. State uncertainty instead of guessing.
- If the request clearly needs the organization's documents, workspace files or
  file operations, say which capability is required instead of inventing one.
- Answer in the user's language. Be concise unless the user asks for depth.
`

func newGeneralAgent(ctx context.Context, cfg *config.Config, chatModel model.BaseChatModel) (adk.Agent, error) {
	return newSpecialistWithModel(
		ctx,
		cfg,
		GeneralAgentName,
		generalDescription,
		generalInstruction,
		nil,
		nil,
		chatModel,
		generalMaxIterations,
	)
}
