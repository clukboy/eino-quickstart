package stream

import (
	"context"
	"errors"
	"io"
	"strings"

	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type roundResult struct {
	Assistant   string
	RoutedTo    string
	Errors      []error
	Interrupted *adk.AgentEvent
}

func forwardRound(ctx context.Context, client chan<- *types.ChatEvent, sessionID string, iter *adk.AsyncIterator[*adk.AgentEvent], abortOnErr bool) (res roundResult, clientGone bool, fatal error) {
	var assistant strings.Builder

	defer func() { res.Assistant = assistant.String() }()

	for {
		event, ok := iter.Next()
		if !ok {
			return res, false, nil
		}

		if event.Action != nil && event.Action.Interrupted != nil {
			res.Interrupted = event
			return res, false, nil
		}

		if event.Err != nil {
			res.Errors = append(res.Errors, event.Err)
			if abortOnErr {
				return res, false, event.Err
			}
		}

		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}

		output := event.Output.MessageOutput

		if output.Role == schema.Tool {
			if output.ToolName != "" {
				res.RoutedTo = output.ToolName
			}
			continue
		}

		if output.IsStreaming {
			if output.MessageStream == nil {
				continue
			}

			alive, err := forwardStream(ctx, client, &assistant, &res, sessionID, event.AgentName, output)
			if err != nil {
				res.Errors = append(res.Errors, err)
				if abortOnErr {
					return res, false, err
				}
				continue
			}
			if !alive {
				return res, true, nil
			}
			continue
		}

		if output.Message == nil {
			continue
		}

		recordToolCalls(&res, output.Message.ToolCalls)
		if output.Message.Content == "" {
			continue
		}

		assistant.WriteString(output.Message.Content)
		if !emit(ctx, client, &types.ChatEvent{
			Type:      eventMessage,
			SessionID: sessionID,
			Agent:     event.AgentName,
			Content:   output.Message.Content,
		}) {
			return res, true, nil
		}
	}
}

func forwardStream(ctx context.Context, client chan<- *types.ChatEvent, assistant *strings.Builder, res *roundResult, sessionID, agentName string, output *adk.MessageVariant) (bool, error) {
	for {
		message, err := output.MessageStream.Recv()
		if errors.Is(err, io.EOF) {
			return true, nil
		}
		if err != nil {
			return true, err
		}

		recordToolCalls(res, message.ToolCalls)

		// 带 tool call 的分片 Content 为空，不能当成"没内容"跳过前先记名字。
		if message.Content == "" {
			continue
		}

		assistant.WriteString(message.Content)
		if !emit(ctx, client, &types.ChatEvent{
			Type:      eventMessage,
			SessionID: sessionID,
			Agent:     agentName,
			Content:   message.Content,
		}) {
			return false, nil
		}
	}
}

func recordToolCalls(res *roundResult, calls []schema.ToolCall) {
	for _, call := range calls {
		if name := call.Function.Name; name != "" {
			res.RoutedTo = name
		}
	}
}

// routingLabel 把路由结果映射成日志里的稳定取值。
// 空值记 "direct"：root 没调任何工具，自己答了 —— 这违反路由契约，用它可以统计
// 违规率（代码层不拦截，拦截需要缓冲全部输出，会毁掉流式体验）。
func routingLabel(routedTo string) string {
	if routedTo == "" {
		return "direct"
	}
	return routedTo
}
