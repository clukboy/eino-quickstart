package middleware

import (
	"context"
	"eino-quickstart/internal/platform/auth"
	toolpolicy "eino-quickstart/internal/tool"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
)

type Approval interface {
	Create(ctx context.Context, sessionID, turnID, actorSubject, toolName, args string) (string, error)
	Consume(ctx context.Context, id, sessionID, actorSubject, toolName, args string) error
}

type Policy struct {
	*adk.BaseChatModelAgentMiddleware
	AllowedTools map[string]struct{}
	RequireShell bool
	RequireWrite bool
	Approvals    Approval
}

func NewPolicy(allowedTools []string, requireShell, requireWrite bool, approvals Approval) *Policy {
	allowed := make(map[string]struct{}, len(allowedTools))
	for _, t := range allowedTools {
		allowed[t] = struct{}{}
	}
	return &Policy{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		AllowedTools:                 allowed,
		RequireShell:                 requireShell,
		RequireWrite:                 requireWrite,
		Approvals:                    approvals,
	}
}

func (p *Policy) WrapInvokableToolCall(_ context.Context, endpoint adk.InvokableToolCallEndpoint, tc *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	if !p.isAllowed(tc.Name) {
		return func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
			return "", fmt.Errorf("tool %q is not allowed by policy", tc.Name)
		}, nil
	}
	guarded := p.requiresApproval(tc.Name)
	if !guarded {
		return endpoint, nil
	}
	return func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
		identity, ok := auth.IdentityFromContext(ctx)
		if !ok {
			return "", fmt.Errorf(
				"authenticated identity is required for tool %q",
				tc.Name,
			)
		}
		sessionID, _ := ctx.Value(sessionContextKey{}).(string)
		if sessionID == "" {
			return "", fmt.Errorf(
				"session ID is required for tool %q",
				tc.Name,
			)
		}

		turnID, ok := TurnIDFromContext(ctx)
		if !ok {
			return "", fmt.Errorf(
				"chat turn ID is required for tool %q",
				tc.Name,
			)
		}

		wasInterrupted, hasState, approvalID := tool.GetInterruptState[string](ctx)

		if wasInterrupted {
			if !hasState || approvalID == "" {
				return "", fmt.Errorf(
					"approval state is missing for tool %q",
					tc.Name,
				)
			}

			if p.Approvals == nil {
				return "", fmt.Errorf("approval service is unavailable")
			}

			if err := p.Approvals.Consume(
				ctx,
				approvalID,
				sessionID,
				identity.Subject,
				tc.Name,
				args,
			); err != nil {
				return "", err
			}

			return endpoint(ctx, args, opts...)
		}

		if p.Approvals == nil {
			return "", fmt.Errorf("tool %q requires approval", tc.Name)
		}

		approvalID, err := p.Approvals.Create(ctx, sessionID, turnID, identity.Subject, tc.Name, args)
		if err != nil {
			return "", fmt.Errorf("create approval: %w", err)
		}
		return "", tool.Interrupt(ctx, map[string]string{
			"approval_id": approvalID,
			"tool":        tc.Name,
		})
	}, nil
}

func (p *Policy) isAllowed(name string) bool {
	_, allowed := p.AllowedTools[name]
	return allowed
}

func (p *Policy) requiresApproval(name string) bool {
	risk, known := toolpolicy.RiskFor(name)
	if !known {
		return true
	}

	switch risk {
	case toolpolicy.RiskWrite:
		return p.RequireWrite
	case toolpolicy.RiskHigh:
		return p.RequireShell
	default:
		return false
	}
}

type sessionContextKey struct{}

func WithSession(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionContextKey{}, sessionID)
}

type turnContextKey struct{}

func WithTurn(ctx context.Context, turnID string) context.Context {
	return context.WithValue(
		ctx,
		turnContextKey{},
		turnID,
	)
}

func TurnIDFromContext(ctx context.Context) (string, bool) {
	turnID, ok := ctx.Value(turnContextKey{}).(string)
	return turnID, ok && turnID != ""
}
