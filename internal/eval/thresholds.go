package eval

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Thresholds 是发布门禁：本轮评测的汇总指标必须落在这个区间内。
//
// 每一项都对应一个「配错了也看不出来」的风险，所以默认值都取保守方向：
// 未配置的门禁项不会静默放行，而是由 LoadThresholds 拦下来。
type Thresholds struct {
	MinPassRate     float64 `yaml:"minPassRate"`
	MinRecallAtK    float64 `yaml:"minRecallAtK"`
	MinMRR          float64 `yaml:"minMRR"`
	MaxACLLeakCount int     `yaml:"maxACLLeakCount"`
	MaxP95LatencyMS int64   `yaml:"maxP95LatencyMS"`
}

// LoadThresholds 读取门禁配置。
//
// 一个空文件、或者漏配全部质量项，在这里就是错误而不是"全部通过"：
// 门禁最危险的失效方式是它以为自己在把关，实际什么都没拦。
func LoadThresholds(path string) (Thresholds, error) {
	var config Thresholds

	data, err := os.ReadFile(path)
	if err != nil {
		return config, fmt.Errorf("eval: 读取门禁配置: %w", err)
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return config, fmt.Errorf("eval: 解析门禁配置 %s: %w", path, err)
	}
	if config.MinPassRate <= 0 && config.MinRecallAtK <= 0 {
		return config, fmt.Errorf(
			"eval: %s 未配置质量门禁（minPassRate / minRecallAtK 至少要有其一）",
			path,
		)
	}
	return config, nil
}

// Check 逐项比对，返回全部违规项。
//
// 不遇到第一个违规就返回，是因为门禁的用法是"改完参数跑一次"：一次看到所有
// 不达标项，比修一个跑一次快得多。
func (t Thresholds) Check(summary Summary) []string {
	var violations []string

	if t.MinPassRate > 0 && summary.PassRate < t.MinPassRate {
		violations = append(violations, fmt.Sprintf(
			"通过率 %.3f 低于门槛 %.3f", summary.PassRate, t.MinPassRate,
		))
	}
	if t.MinRecallAtK > 0 && summary.RecallAtK < t.MinRecallAtK {
		violations = append(violations, fmt.Sprintf(
			"Recall@K %.3f 低于门槛 %.3f", summary.RecallAtK, t.MinRecallAtK,
		))
	}
	if t.MinMRR > 0 && summary.MRR < t.MinMRR {
		violations = append(violations, fmt.Sprintf(
			"MRR %.3f 低于门槛 %.3f", summary.MRR, t.MinMRR,
		))
	}
	// 泄漏项即使配置为 0 也要比：0 是这里唯一可接受的值。
	if summary.ACLLeakCount > t.MaxACLLeakCount {
		violations = append(violations, fmt.Sprintf(
			"ACL 泄漏 %d 条，超过上限 %d", summary.ACLLeakCount, t.MaxACLLeakCount,
		))
	}
	if t.MaxP95LatencyMS > 0 && summary.P95LatencyMS > t.MaxP95LatencyMS {
		violations = append(violations, fmt.Sprintf(
			"P95 延迟 %dms 超过上限 %dms", summary.P95LatencyMS, t.MaxP95LatencyMS,
		))
	}

	return violations
}
