package middleware

import "testing"

func TestPolicyAllowsOnlyConfiguredTools(t *testing.T) {
	policy := NewPolicy(
		[]string{"read_file", "list_dir"},
		true,
		true,
		nil,
	)

	if !policy.isAllowed("read_file") {
		t.Fatal("read_file should be allowed")
	}
	if policy.isAllowed("shell") {
		t.Fatal("shell should not be allowed")
	}
	if policy.isAllowed("future_tool") {
		t.Fatal("unknown tools should not be allowed")
	}
}

func TestPolicyApprovalByRisk(t *testing.T) {
	policy := NewPolicy(
		[]string{"read_file", "write_file", "shell"},
		true,
		false,
		nil,
	)

	if policy.requiresApproval("read_file") {
		t.Fatal("read_file should not require approval")
	}
	if policy.requiresApproval("write_file") {
		t.Fatal("write_file should not require approval")
	}
	if !policy.requiresApproval("shell") {
		t.Fatal("shell should require approval")
	}
}
