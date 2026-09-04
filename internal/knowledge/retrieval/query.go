package retrieval

import (
	"eino-quickstart/internal/knowledge"
	"regexp"
	"strings"
)

type QueryInfo struct {
	Raw string

	Model    string
	HasModel bool

	Topics []string

	Keywords []string

	Intent string
}

var modelPattern = regexp.MustCompile(`(?i)(?:^|[^A-Z0-9])(H\d+[A-Z0-9]*)(?:[^A-Z0-9]|$)`)

func ParseQuery(query string) QueryInfo {
	query = strings.TrimSpace(query)

	info := QueryInfo{
		Raw: query,
	}

	if query == "" {
		return info
	}

	match := modelPattern.FindStringSubmatch(query)

	if len(match) >= 2 {
		info.Model = strings.ToUpper(match[1])
		info.HasModel = true
	}
	info.Topics = knowledge.DetectTopics("", query)

	info.Keywords = extractKeywords(query)

	info.Intent = detectIntent(query)

	return info
}

func detectIntent(query string) string {
	switch {
	case strings.Contains(query, "怎么安装"),
		strings.Contains(query, "如何安装"),
		strings.Contains(query, "安装方法"),
		strings.Contains(query, "安装方式"):
		return "installation"

	case strings.Contains(query, "怎么办"),
		strings.Contains(query, "怎么解决"),
		strings.Contains(query, "故障"),
		strings.Contains(query, "异响"),
		strings.Contains(query, "异常"):
		return "problem_solving"

	case strings.Contains(query, "参数"),
		strings.Contains(query, "规格"):
		return "specification"

	case strings.Contains(query, "怎么调"),
		strings.Contains(query, "如何调节"),
		strings.Contains(query, "调节方法"):
		return "adjustment"

	default:
		return ""
	}
}
