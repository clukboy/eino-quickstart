package eval

import (
	"fmt"
	"os"

	"github.com/goccy/go-json"
)

type Thresholds struct {
	MinPassRate     float64 `yaml:"minPassRate"`
	MinRecallAtK    float64 `yaml:"minRecallAtK"`
	MaxACLLeakCount int     `yaml:"maxACLLeakCount"`
	MaxP95LatencyMS int64   `yaml:"maxP95LatencyMS"`
}

func WriteReport(path string, report Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func CheckThresholds(
	summary Summary,
	thresholds Thresholds,
) error {
	if summary.PassRate < thresholds.MinPassRate {
		return fmt.Errorf(
			"pass rate %.3f is below %.3f",
			summary.PassRate,
			thresholds.MinPassRate,
		)
	}

	if summary.RecallAtK < thresholds.MinRecallAtK {
		return fmt.Errorf(
			"recall@k %.3f is below %.3f",
			summary.RecallAtK,
			thresholds.MinRecallAtK,
		)
	}

	if summary.ACLLeakCount > thresholds.MaxACLLeakCount {
		return fmt.Errorf(
			"ACL leak count %d exceeds %d",
			summary.ACLLeakCount,
			thresholds.MaxACLLeakCount,
		)
	}

	if summary.P95LatencyMS > thresholds.MaxP95LatencyMS {
		return fmt.Errorf(
			"p95 latency %dms exceeds %dms",
			summary.P95LatencyMS,
			thresholds.MaxP95LatencyMS,
		)
	}

	return nil
}
