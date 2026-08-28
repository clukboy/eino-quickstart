package eval

import (
	"time"

	harnesstool "eino-quickstart/internal/tool"
)

type ToolPolicyEvaluator struct {
	AllowedTools map[string]struct{}
	RequireShell bool
	RequireWrite bool
}

func (e ToolPolicyEvaluator) Evaluate(
	test ToolPolicyCase,
) CaseResult {
	startedAt := time.Now()

	_, allowed := e.AllowedTools[test.Tool]

	result := CaseResult{
		ID:       test.ID,
		Duration: time.Since(startedAt).Milliseconds(),
		Details: map[string]any{
			"tool": test.Tool,
		},
	}

	if allowed != test.Allowed {
		result.Error = "unexpected allow decision"
		return result
	}

	risk, known := harnesstool.RiskFor(test.Tool)

	requiresApproval := false
	if known {
		switch risk {
		case harnesstool.RiskWrite:
			requiresApproval = e.RequireWrite
		case harnesstool.RiskHigh:
			requiresApproval = e.RequireShell
		}
	}

	if test.Allowed &&
		requiresApproval != test.RequiresApproval {
		result.Error = "unexpected approval requirement"
		return result
	}

	result.Passed = true
	return result
}
