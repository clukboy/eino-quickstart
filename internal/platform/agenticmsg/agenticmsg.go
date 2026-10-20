// Package agenticmsg 收敛 eino agentic 消息与「纯文本」之间的两个方向。
//
// agentic 体系里一条消息的正文不再是单个 Content 字符串，而是 ContentBlocks：
// 用户说的在 UserInputText 里，模型说的在 AssistantGenText 里，工具结果在
// FunctionToolResult 里。而库里只存 (role, content) 两列，所以：
//
//   - 读历史要把 (role, text) 还原成 blocks（Store.History 用 FromStored）；
//   - 推 SSE 要把 blocks 抽回文本（forwardRound 用 Text）。
//
// 这两个方向必须共用同一套映射。分开写会有一个很具体的后果：写进历史的是
// UserInputText、而读回来时按 AssistantGenText 还原，模型看到自己"说过"用户的
// 话，多轮之后就开始自问自答。
//
// 放在 platform 下而不是 application 下，是因为使用方横跨两层
// （platform/persistence 与 transport），放在 application 会让 persistence
// 反向依赖上层。
package agenticmsg

import (
	"strings"

	"github.com/cloudwego/eino/schema"
)

// FromStored 把库里存的 (role, content) 还原成一条 agentic 消息。
//
// 未知 role 返回 nil，由调用方跳过 —— 宁可少一条历史，也不要把一条不知来历的
// 消息按 user 塞给模型（那等于让数据库里的脏数据冒充用户提问）。
func FromStored(role string, content string) *schema.AgenticMessage {
	switch schema.AgenticRoleType(role) {
	case schema.AgenticRoleTypeAssistant:
		return &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.AssistantGenText{Text: content}),
			},
		}
	case schema.AgenticRoleTypeUser:
		return schema.UserAgenticMessage(content)
	case schema.AgenticRoleTypeSystem:
		return schema.SystemAgenticMessage(content)
	default:
		return nil
	}
}

// Text 抽出一条消息里「人看得见的正文」。
//
// 只认两种 block：UserInputText（用户说的话）与 AssistantGenText（模型说的话）。
// 其余一律跳过，原因各不相同：
//   - Reasoning：思考过程不属于回答，混进正文会让前端出现两遍答案；
//   - FunctionToolCall / FunctionToolResult：是过程细节，不是给人的内容；
//     工具结果尤其不能漏 —— 它是 role=user 的消息，抽出来就等于把工具原始
//     输出当成用户发言回显给客户端。
//
// 用「指针非空」而不是 block.Type 判断，是因为流式分片的 Type 依赖生产方
// 正确填充，而指针是 concat 与增量两路都会保证的。
func Text(msg *schema.AgenticMessage) string {
	if msg == nil {
		return ""
	}

	var sb strings.Builder
	for _, block := range msg.ContentBlocks {
		if block == nil {
			continue
		}

		switch {
		case block.UserInputText != nil:
			sb.WriteString(block.UserInputText.Text)
		case block.AssistantGenText != nil:
			sb.WriteString(block.AssistantGenText.Text)
		}
	}

	return sb.String()
}
