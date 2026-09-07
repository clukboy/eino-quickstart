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

// 支持：
// H105
// H105G
// H17S
// T206
// WF10
// WF10A
//
// 要求：字母开头 + 数字 + 可选字母数字后缀。
var modelPattern = regexp.MustCompile(
	`(?i)(?:^|[^A-Z0-9])([A-Z]{1,6}\d+[A-Z0-9]*)(?:[^A-Z0-9]|$)`,
)

var asciiTokenPattern = regexp.MustCompile(
	`(?i)[a-z0-9]+(?:[-_][a-z0-9]+)*`,
)

var cjkRunPattern = regexp.MustCompile(
	`[\p{Han}]+`,
)

var keywordStopWords = map[string]struct{}{
	"怎么":  {},
	"如何":  {},
	"什么":  {},
	"是否":  {},
	"可以":  {},
	"能够":  {},
	"的是":  {},
	"一个":  {},
	"这个":  {},
	"那个":  {},
	"有没有": {},
	"请问":  {},
	"帮我":  {},
	"一下":  {},
	"呢":   {},
	"吗":   {},
	"啊":   {},
	"呀":   {},
	"的":   {},
	"是":   {},
	"有":   {},
	"后":   {},
	"了":   {},
}

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

	info.Topics = knowledge.DetectTopics(
		"",
		query,
	)

	info.Keywords = extractKeywords(query)

	info.Intent = detectIntent(query)

	return info
}

// BuildKeywordQuery converts a structured query into a retrieval-oriented
// keyword query.
//
// Example:
//
// # H105安装后关门有异响怎么办
//
// becomes approximately:
//
// H105 安装 装后 后关 关门 门有 有异 异响 怎么 么办 troubleshooting installation
func BuildKeywordQuery(info QueryInfo) string {
	terms := make([]string, 0, 32)

	if info.Model != "" {
		terms = append(terms, info.Model)
	}

	terms = append(terms, info.Keywords...)
	terms = append(terms, info.Topics...)

	if info.Intent != "" {
		terms = append(terms, info.Intent)
	}

	return strings.Join(uniqueNonEmpty(terms), " ")
}

func extractKeywords(query string) []string {
	query = strings.TrimSpace(query)

	if query == "" {
		return nil
	}

	result := make([]string, 0, 32)
	seen := make(map[string]struct{})

	add := func(value string) {
		value = strings.TrimSpace(
			strings.ToLower(value),
		)

		if value == "" {
			return
		}

		if len([]rune(value)) < 2 {
			return
		}

		if _, exists := keywordStopWords[value]; exists {
			return
		}

		if _, exists := seen[value]; exists {
			return
		}

		seen[value] = struct{}{}
		result = append(result, value)
	}

	// ASCII / 产品型号 / 英文关键词。
	for _, token := range asciiTokenPattern.FindAllString(query, -1) {
		add(token)
	}

	// 中文连续文本。
	for _, run := range cjkRunPattern.FindAllString(query, -1) {
		runes := []rune(run)

		// 优先提取 4 / 3 / 2 字词组。
		// 这样不依赖 PostgreSQL 中文分词。
		for n := 4; n >= 2; n-- {
			if len(runes) < n {
				continue
			}

			for i := 0; i+n <= len(runes); i++ {
				add(string(runes[i : i+n]))
			}
		}
	}

	return result
}

func uniqueNonEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))

	for _, value := range values {
		value = strings.TrimSpace(value)

		if value == "" {
			continue
		}

		if _, exists := seen[value]; exists {
			continue
		}

		seen[value] = struct{}{}
		result = append(result, value)
	}

	return result
}

func detectIntent(query string) string {
	query = strings.ToLower(
		strings.TrimSpace(query),
	)

	switch {
	case strings.Contains(query, "怎么安装"),
		strings.Contains(query, "如何安装"),
		strings.Contains(query, "安装方法"),
		strings.Contains(query, "安装方式"),
		strings.Contains(query, "安装步骤"):
		return "installation"

	case strings.Contains(query, "怎么办"),
		strings.Contains(query, "怎么解决"),
		strings.Contains(query, "故障"),
		strings.Contains(query, "异响"),
		strings.Contains(query, "异常"),
		strings.Contains(query, "排查"),
		strings.Contains(query, "解决方法"):
		return "problem_solving"

	case strings.Contains(query, "参数"),
		strings.Contains(query, "规格"),
		strings.Contains(query, "尺寸"):
		return "specification"

	case strings.Contains(query, "怎么调"),
		strings.Contains(query, "如何调节"),
		strings.Contains(query, "调节方法"),
		strings.Contains(query, "调整方法"):
		return "adjustment"

	case strings.Contains(query, "怎么用"),
		strings.Contains(query, "如何使用"),
		strings.Contains(query, "使用方法"):
		return "usage"

	default:
		return ""
	}
}
