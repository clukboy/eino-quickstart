package agenticmsg

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

// 存的 (role, text) 读回来之后，必须还能被 Text 抽回同一段文本 —— 这是
// History 与 SSE 转发共用一套映射的全部意义。映射写反了（user 存进去、
// assistant 读出来）在这里会立刻暴露。
func TestFromStoredRoundTripsThroughText(t *testing.T) {
	cases := []struct {
		role string
		text string
	}{
		{role: "user", text: "公司的报销流程是什么？"},
		{role: "assistant", text: "先提交 OA，再由直属主管审批。"},
	}

	for _, tc := range cases {
		message := FromStored(tc.role, tc.text)
		if message == nil {
			t.Fatalf("FromStored(%q) = nil", tc.role)
		}
		if message.Role != schema.AgenticRoleType(tc.role) {
			t.Fatalf("FromStored(%q).Role = %q", tc.role, message.Role)
		}
		if got := Text(message); got != tc.text {
			t.Fatalf("Text(FromStored(%q)) = %q, want %q", tc.role, got, tc.text)
		}
	}
}

// 未知 role 返回 nil，让调用方跳过这条历史，而不是把它当用户发言。
func TestFromStoredRejectsUnknownRole(t *testing.T) {
	if message := FromStored("tool", "whatever"); message != nil {
		t.Fatalf("FromStored(tool) = %+v, want nil", message)
	}
}

// 工具结果是 role=user 的消息，它的正文在 FunctionToolResult 里，绝不能
// 被抽成自然语言回显给客户端。
func TestTextSkipsToolResults(t *testing.T) {
	message := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeUser,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.FunctionToolResult{
				Name: "search_knowledge",
				Content: []*schema.FunctionToolResultContentBlock{
					{
						Type: schema.FunctionToolResultContentBlockTypeText,
						Text: &schema.UserInputText{Text: "raw chunk from vector db"},
					},
				},
			}),
		},
	}

	if got := Text(message); got != "" {
		t.Fatalf("Text(tool result) = %q, want empty", got)
	}
}

// reasoning 是思考过程，不属于回答 —— 混进正文会让前端出现两遍内容。
func TestTextSkipsReasoning(t *testing.T) {
	message := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.Reasoning{Text: "let me think"}),
			schema.NewContentBlock(&schema.AssistantGenText{Text: "answer"}),
		},
	}

	if got := Text(message); got != "answer" {
		t.Fatalf("Text = %q, want only the gen text", got)
	}
}

func TestTextHandlesNilMessage(t *testing.T) {
	if got := Text(nil); got != "" {
		t.Fatalf("Text(nil) = %q, want empty", got)
	}
}
