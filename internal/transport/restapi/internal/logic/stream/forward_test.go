package stream

import (
	"context"
	"errors"
	"testing"

	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// newRound 用一对迭代器拼出一轮事件流：send 完立刻关，模拟 Agent 正常结束。
func newRound(events ...*adk.AgentEvent) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	for _, event := range events {
		gen.Send(event)
	}
	gen.Close()
	return iter
}

func streamingEvent(agentName string, chunks ...*schema.Message) *adk.AgentEvent {
	return &adk.AgentEvent{
		AgentName: agentName,
		Output: &adk.AgentOutput{
			MessageOutput: &adk.MessageVariant{
				IsStreaming:   true,
				MessageStream: schema.StreamReaderFromArray(chunks),
			},
		},
	}
}

func drain(client chan *types.ChatEvent) []*types.ChatEvent {
	var events []*types.ChatEvent
	for {
		select {
		case event := <-client:
			events = append(events, event)
		default:
			return events
		}
	}
}

func TestForwardRoundCapturesRoutingAndContent(t *testing.T) {
	// 第一个分片只带 tool call（Content 为空），第二个分片才是正文 —— 这正是
	// 真实流式的样子，也是「不能因为 Content 为空就跳过、否则记不到路由」的原因。
	iter := newRound(streamingEvent("root_agent",
		schema.AssistantMessage("", []schema.ToolCall{
			{Function: schema.FunctionCall{Name: "knowledge_agent"}},
		}),
		schema.AssistantMessage("hello", nil),
	))

	client := make(chan *types.ChatEvent, 16)
	res, clientGone, fatal := forwardRound(context.Background(), client, "session-1", iter, false)

	if fatal != nil {
		t.Fatalf("unexpected fatal error: %v", fatal)
	}
	if clientGone {
		t.Fatal("clientGone should be false")
	}
	if res.RoutedTo != "knowledge_agent" {
		t.Fatalf("RoutedTo = %q, want knowledge_agent", res.RoutedTo)
	}
	if res.Assistant != "hello" {
		t.Fatalf("Assistant = %q, want hello", res.Assistant)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}

	events := drain(client)
	if len(events) != 1 || events[0].Content != "hello" || events[0].Type != eventMessage {
		t.Fatalf("unexpected frames: %+v", events)
	}
	if events[0].Agent != "root_agent" {
		t.Fatalf("frame agent = %q, want root_agent", events[0].Agent)
	}
}

// Event 级错误在 abortOnErr=false 时只收集、不中断，后面的正常输出仍然要转发。
func TestForwardRoundCollectsErrorsWhenNotAborting(t *testing.T) {
	iter := newRound(
		&adk.AgentEvent{AgentName: "root_agent", Err: errors.New("boom")},
		streamingEvent("root_agent", schema.AssistantMessage("still here", nil)),
	)

	client := make(chan *types.ChatEvent, 16)
	res, _, fatal := forwardRound(context.Background(), client, "session-1", iter, false)

	if fatal != nil {
		t.Fatalf("fatal should be nil when abortOnErr is false: %v", fatal)
	}
	if len(res.Errors) != 1 {
		t.Fatalf("Errors = %v, want exactly one", res.Errors)
	}
	if res.Assistant != "still here" {
		t.Fatalf("Assistant = %q, want the content emitted after the error", res.Assistant)
	}
}

func TestForwardRoundAbortsOnError(t *testing.T) {
	boom := errors.New("boom")
	iter := newRound(
		&adk.AgentEvent{AgentName: "root_agent", Err: boom},
		streamingEvent("root_agent", schema.AssistantMessage("never reached", nil)),
	)

	client := make(chan *types.ChatEvent, 16)
	res, _, fatal := forwardRound(context.Background(), client, "session-1", iter, true)

	if !errors.Is(fatal, boom) {
		t.Fatalf("fatal = %v, want boom", fatal)
	}
	if res.Assistant != "" {
		t.Fatalf("Assistant = %q, want empty", res.Assistant)
	}
}

// 中断必须被单独识别出来：调用方要据此落 checkpoint，而不是当成空回答去兜底。
func TestForwardRoundReportsInterrupt(t *testing.T) {
	iter := newRound(
		streamingEvent("root_agent", schema.AssistantMessage("partial", nil)),
		&adk.AgentEvent{
			AgentName: "root_agent",
			Action:    &adk.AgentAction{Interrupted: &adk.InterruptInfo{}},
		},
	)

	client := make(chan *types.ChatEvent, 16)
	res, _, fatal := forwardRound(context.Background(), client, "session-1", iter, false)

	if fatal != nil {
		t.Fatalf("unexpected fatal: %v", fatal)
	}
	if res.Interrupted == nil {
		t.Fatal("Interrupted should be set")
	}
	if res.Assistant != "partial" {
		t.Fatalf("Assistant = %q, want the content produced before the interrupt", res.Assistant)
	}
}

// 只有 done、没有 message 是这个 bug 的原始症状。空事件流必须如实返回空正文，
// 由调用方触发兜底 —— 这个测试锁住「不产出」这件事本身是可观测的。
func TestForwardRoundEmptyRound(t *testing.T) {
	iter := newRound()

	client := make(chan *types.ChatEvent, 16)
	res, clientGone, fatal := forwardRound(context.Background(), client, "session-1", iter, false)

	if fatal != nil || clientGone {
		t.Fatalf("unexpected fatal=%v clientGone=%v", fatal, clientGone)
	}
	if res.Assistant != "" {
		t.Fatalf("Assistant = %q, want empty", res.Assistant)
	}
	if res.RoutedTo != "" {
		t.Fatalf("RoutedTo = %q, want empty", res.RoutedTo)
	}
	if routingLabel(res.RoutedTo) != "direct" {
		t.Fatalf("routingLabel = %q, want direct", routingLabel(res.RoutedTo))
	}
}
