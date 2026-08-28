package tool

import "testing"

func TestRiskFor(t *testing.T) {
	tests := map[string]RiskLevel{
		"read_file":    RiskRead,
		"list_dir":     RiskRead,
		"write_file":   RiskWrite,
		"shell":        RiskHigh,
		"unknown_tool": "",
	}
	for name, want := range tests {
		if got, known := RiskFor(name); got != want || known != (name != "unknown_tool") {
			t.Errorf("RiskFor(%q) = %q, want %q", name, got, want)
		}
	}
}
