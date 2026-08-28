package middleware

import (
	"context"
	"eino-quickstart/internal/application/contextmgr"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
)

type ToolOutputLimit struct {
	*adk.BaseChatModelAgentMiddleware
	context *contextmgr.Manager
}

func NewToolOutputLimit(maxBytes int) *ToolOutputLimit {
	return &ToolOutputLimit{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		context:                      contextmgr.New(0, maxBytes),
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
