package chat

import (
	"context"
	"errors"
	"io"
	"strings"

	"eino-quickstart/internal/platform/agenticmsg"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// agenticEvent 是 TypedRunner[*schema.AgenticMessage] 产出的事件类型。
//
// 不能用 adk.AgentEvent：那个别名等价于 TypedAgentEvent[*schema.Message]，
// 它的 MessageOutput 里装的是普通消息（只有 Content 字符串），取不到 blocks。
// 用错类型是编译期错误，不会漏到线上。
type agenticEvent = adk.TypedAgentEvent[*schema.AgenticMessage]

// round 是一轮跑完之后，落库需要的事实。
type round struct {
	Assistant string
	Blocks    []agenticmsg.Block
	Errors    []error
}

func pump(ctx context.Context, iter *adk.AsyncIterator[*agenticEvent], sessionID string, maxBlockBytes int, sink Sink) (res round, clientGone bool) {
	var assistant strings.Builder

	defer func() { res.Assistant = assistant.String() }()

	for {
		event, ok := iter.Next()
		if !ok {
			return res, false
		}

		if event.Action != nil && event.Action.Interrupted != nil {
			res.Errors = append(res.Errors, errors.New("unexpected agent interruption"))
			return res, false
		}

		if event.Err != nil {
			res.Errors = append(res.Errors, event.Err)
		}

		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}

		output := event.Output.MessageOutput

		if output.IsStreaming {
			if output.MessageStream == nil {
				continue
			}

			blocks, alive, err := pumpStream(
				ctx, &assistant, sessionID, maxBlockBytes, output, sink,
			)
			res.Blocks = append(res.Blocks, blocks...)
			if err != nil {
				res.Errors = append(res.Errors, err)
				continue
			}
			if !alive {
				return res, true
			}
			continue
		}

		blocks := agenticmsg.Sealed(agenticmsg.Blocks(output.Message, maxBlockBytes))
		res.Blocks = append(res.Blocks, blocks...)
		if !emitToolEvents(blocks, sessionID, sink) {
			return res, true
		}

		text := agenticmsg.Text(output.Message)
		if text == "" {
			continue
		}

		assistant.WriteString(text)
		if !sink(Event{Kind: KindMessage, SessionID: sessionID, Content: text}) {
			return res, true
		}
	}
}

func pumpStream(_ context.Context, assistant *strings.Builder, sessionID string, maxBlockBytes int, output *adk.TypedMessageVariant[*schema.AgenticMessage], sink Sink) ([]agenticmsg.Block, bool, error) {
	var pending []agenticmsg.Block

	for {
		message, err := output.MessageStream.Recv()
		if errors.Is(err, io.EOF) {
			return agenticmsg.Sealed(pending), true, nil
		}
		if err != nil {
			return agenticmsg.Sealed(pending), true, err
		}

		added := len(pending)
		pending = agenticmsg.Merge(pending, agenticmsg.Blocks(message, maxBlockBytes))
		if !emitToolEvents(pending[added:], sessionID, sink) {
			return agenticmsg.Sealed(pending), false, nil
		}

		text := agenticmsg.Text(message)
		if text == "" {
			continue
		}

		assistant.WriteString(text)
		if !sink(Event{Kind: KindMessage, SessionID: sessionID, Content: text}) {
			return agenticmsg.Sealed(pending), false, nil
		}
	}
}

// emitToolEvents 把工具块推成 tool 事件。
//
// 传进来的应当只有**新出现的块**（流式里是 pending 这次增长的那一段）：同一
// index 的后续分片已经并进老块，不会再出现在这里，所以一次调用只出一条帧。
//
// 返回 false 表示客户端断开。
func emitToolEvents(blocks []agenticmsg.Block, sessionID string, sink Sink) bool {
	for _, block := range blocks {
		var summary string
		switch block.Type {
		case agenticmsg.BlockToolCall:
			summary = "开始调用"
		case agenticmsg.BlockToolResult, agenticmsg.BlockToolSearch,
			agenticmsg.BlockApprovalRequest, agenticmsg.BlockApprovalResponse:
			summary = block.Summary
		default:
			continue
		}

		if !sink(Event{Kind: KindTool, SessionID: sessionID, Tool: block.Name, Summary: summary}) {
			return false
		}
	}

	return true
}
