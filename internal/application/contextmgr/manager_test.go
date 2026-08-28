package contextmgr

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestPrepareKeepsNewestMessages(t *testing.T) {
	manager := New(2, 0)

	messages := []*schema.Message{
		schema.UserMessage("first"),
		schema.AssistantMessage("second", nil),
		schema.UserMessage("third"),
	}

	result := manager.Prepare(messages)

	if len(result) != 2 {
		t.Fatalf("len(result) = %d, want 2", len(result))
	}
	if result[0].Content != "second" {
		t.Fatalf("first retained message = %q", result[0].Content)
	}
	if result[1].Content != "third" {
		t.Fatalf("second retained message = %q", result[1].Content)
	}
}

func TestTrimToolOutputDoesNotExceedLimit(t *testing.T) {
	manager := New(0, 20)

	result := manager.TrimToolOutput(strings.Repeat("x", 100))

	if len(result) > 20 {
		t.Fatalf("output length = %d, exceeds 20", len(result))
	}
}
