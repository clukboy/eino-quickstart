package contextmgr

import (
	"github.com/cloudwego/eino/schema"
)

const toolOutputTruncatedSuffix = "\n...[tool output truncated by harness]"

type Manager struct {
	MaxMessages        int
	MaxToolOutputBytes int
}

func New(maxMessages, maxToolOutputBytes int) *Manager {
	return &Manager{MaxMessages: maxMessages, MaxToolOutputBytes: maxToolOutputBytes}
}

// Prepare 把历史裁到最近的 MaxMessages 条。
//
// 裁剪是「切掉最旧的若干条」，不是「从头保留」—— 对话的有效上下文永远在末尾，
// 而且 AgenticMessage 之间可能有 tool_call / tool_result 的配对关系，从中间切
// 会留下一个没有对应调用的工具结果，模型那边会直接报 400。
func (m *Manager) Prepare(history []*schema.AgenticMessage) []*schema.AgenticMessage {
	if m.MaxMessages <= 0 || len(history) <= m.MaxMessages {
		return history
	}
	return history[len(history)-m.MaxMessages:]
}

func (m *Manager) TrimToolOutput(content string) string {
	if m.MaxToolOutputBytes <= 0 ||
		len(content) <= m.MaxToolOutputBytes {
		return content
	}

	if m.MaxToolOutputBytes <= len(toolOutputTruncatedSuffix) {
		return toolOutputTruncatedSuffix[:m.MaxToolOutputBytes]
	}

	limit := m.MaxToolOutputBytes - len(toolOutputTruncatedSuffix)
	return content[:limit] + toolOutputTruncatedSuffix
}
