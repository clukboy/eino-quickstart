package contextmgr

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestPrepareKeepsNewestMessages(t *testing.T) {
	manager := New(2, 0)

	messages := []*schema.AgenticMessage{
		schema.UserAgenticMessage("first"),
		&schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.AssistantGenText{Text: "second"}),
			},
		},
		schema.UserAgenticMessage("third"),
	}

	result := manager.Prepare(messages)

	if len(result) != 2 {
		t.Fatalf("len(result) = %d, want 2", len(result))
	}
	if got := result[0].ContentBlocks[0].AssistantGenText.Text; got != "second" {
		t.Fatalf("first retained message = %q", got)
	}
	if got := result[1].ContentBlocks[0].UserInputText.Text; got != "third" {
		t.Fatalf("second retained message = %q", got)
	}
}

// MaxMessages 非正数表示「不裁剪」，历史必须原样返回。
func TestPrepareWithoutLimitKeepsEverything(t *testing.T) {
	manager := New(0, 0)

	messages := []*schema.AgenticMessage{
		schema.UserAgenticMessage("first"),
		schema.UserAgenticMessage("second"),
	}

	if result := manager.Prepare(messages); len(result) != 2 {
		t.Fatalf("len(result) = %d, want 2", len(result))
	}
}

func TestTrimToolOutputDoesNotExceedLimit(t *testing.T) {
	manager := New(0, 20)

	result := manager.TrimToolOutput(strings.Repeat("x", 100))

	if len(result) > 20 {
		t.Fatalf("output length = %d, exceeds 20", len(result))
	}
}
