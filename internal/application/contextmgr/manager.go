package contextmgr

import (
	"strings"

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

func (m *Manager) Prepare(history []*schema.Message) []*schema.Message {
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
func (m *Manager) BuildInstruction(base, workspace string) string {
	return strings.TrimSpace(base + "\n\nRuntime context:\n- Workspace: " + workspace + "\n- Filesystem paths must remain inside the workspace.\n- Ask for approval before high-risk operations when required by policy.")
}
