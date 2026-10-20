package middleware

import (
	"context"
	"eino-quickstart/internal/application/contextmgr"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type ToolOutputLimit struct {
	adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]
	context *contextmgr.Manager
}

func NewToolOutputLimit(maxBytes int) *ToolOutputLimit {
	return &ToolOutputLimit{
		TypedChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]{},
		context:                       contextmgr.New(0, maxBytes),
	}
}

func (m *ToolOutputLimit) WrapInvokableToolCall(
	_ context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	_ *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
		output, err := endpoint(ctx, args, opts...)
		return m.context.TrimToolOutput(output), err
	}, nil
}
