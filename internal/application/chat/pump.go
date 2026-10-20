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

		// 没有工具能产生审批中断；出现中断只可能是 runtime 异常，记下来当错误。
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

// pumpStream 消费一段模型的流式输出。
//
// 返回的 blocks 是**这一段流**的块，已经抹掉分片序号：分片序号只在一条消息内
// 唯一，跨消息合并会把不同调用的同序号工具调用拼到一起。
func pumpStream(
	ctx context.Context,
	assistant *strings.Builder,
	sessionID string,
	maxBlockBytes int,
	output *adk.TypedMessageVariant[*schema.AgenticMessage],
	sink Sink,
) ([]agenticmsg.Block, bool, error) {
	var pending []agenticmsg.Block

	for {
		message, err := output.MessageStream.Recv()
		if errors.Is(err, io.EOF) {
			sealed := agenticmsg.Sealed(pending)
			if !emitToolEvents(sealed, sessionID, sink) {
				return sealed, false, nil
			}
			return sealed, true, nil
		}
		if err != nil {
			return agenticmsg.Sealed(pending), true, err
		}

		pending = agenticmsg.Merge(pending, agenticmsg.Blocks(message, maxBlockBytes))

		// 带 tool call 的分片没有任何自然语言正文，Text 返回空串，不外发即可。
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

// emitToolEvents 把一条消息里的工具块推成 tool 事件。
//
// 在**一条消息处理完**之后一次性推，而不是逐片推：工具调用的入参是流式到达的，
// 逐片推会让前端看到十几个「正在调用」。
//
// 返回 false 表示客户端断开。
func emitToolEvents(blocks []agenticmsg.Block, sessionID string, sink Sink) bool {
	for _, block := range blocks {
		var summary string
		switch block.Type {
		case agenticmsg.BlockToolCall:
			summary = "开始调用"
		case agenticmsg.BlockToolResult:
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
