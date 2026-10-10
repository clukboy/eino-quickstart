package eval

import (
	"regexp"
	"strconv"
	"strings"
)

// AnswerCase 是回答层的最小可审计金标。它与召回 Case 分开，避免把“检索有结果”
// 错当成“模型回答有证据”。
type AnswerCase struct {
	ID              string
	Answer          string
	RequiredFacts   []string
	ForbiddenFacts  []string
	AllowedSources  []string
	RequireCitation bool
	NoAnswer        bool
	NoAnswerMarkers []string
}

// Citation 是回答文本里可解析的 [source#chunk-N] 引用。
type Citation struct {
	Source  string
	ChunkID int64
}

// AnswerResult 是逐回答门禁的可读失败明细。
type AnswerResult struct {
	ID                    string
	Passed                bool
	MissingFacts          []string
	ForbiddenFacts        []string
	MissingCitation       bool
	UnauthorizedCitations []string
	NoAnswerViolation     string
	Citations             []Citation
}

var citationPattern = regexp.MustCompile(`\[([^\]\r\n]+)#chunk-([0-9]+)\]`)

// ExtractCitations 只解析知识工具约定的显式引用格式，不把普通 URL、文件名或模型
// 自己编造的“参考资料”当成证据。
func ExtractCitations(answer string) []Citation {
	matches := citationPattern.FindAllStringSubmatch(answer, -1)
	if len(matches) == 0 {
		return nil
	}
	citations := make([]Citation, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		chunkID, err := strconv.ParseInt(match[2], 10, 64)
		if err != nil {
			continue
		}
		source := strings.TrimSpace(match[1])
		key := source + "#" + match[2]
		if source == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		citations = append(citations, Citation{Source: source, ChunkID: chunkID})
	}
	return citations
}

// EvaluateAnswer 校验事实、禁答内容和引用归属。它不评价文风，也不使用模型裁判，
// 目的是给 P22 一个确定性、可接 CI 的底线；有争议的事实仍需人工复核。
func EvaluateAnswer(item AnswerCase) AnswerResult {
	result := AnswerResult{ID: item.ID}
	answer := strings.TrimSpace(item.Answer)
	lowerAnswer := strings.ToLower(answer)

	for _, fact := range item.RequiredFacts {
		fact = strings.TrimSpace(fact)
		if fact != "" && !strings.Contains(lowerAnswer, strings.ToLower(fact)) {
			result.MissingFacts = append(result.MissingFacts, fact)
		}
	}
	for _, fact := range item.ForbiddenFacts {
		fact = strings.TrimSpace(fact)
		if fact != "" && strings.Contains(lowerAnswer, strings.ToLower(fact)) {
			result.ForbiddenFacts = append(result.ForbiddenFacts, fact)
		}
	}

	result.Citations = ExtractCitations(answer)
	if item.NoAnswer {
		markers := item.NoAnswerMarkers
		if len(markers) == 0 {
			markers = []string{"no answer", "insufficient information", "cannot determine", "无法确定", "信息不足"}
		}
		foundMarker := false
		for _, marker := range markers {
			if marker != "" && strings.Contains(lowerAnswer, strings.ToLower(marker)) {
				foundMarker = true
				break
			}
		}
		if !foundMarker {
			result.NoAnswerViolation = "answer does not explicitly abstain"
		}
	} else if item.RequireCitation || len(item.AllowedSources) > 0 {
		if len(result.Citations) == 0 {
			result.MissingCitation = true
		}
	}

	allowed := make(map[string]struct{}, len(item.AllowedSources))
	for _, source := range item.AllowedSources {
		source = strings.TrimSpace(source)
		if source != "" {
			allowed[source] = struct{}{}
		}
	}
	if len(allowed) > 0 {
		for _, citation := range result.Citations {
			if _, ok := allowed[citation.Source]; !ok {
				result.UnauthorizedCitations = append(result.UnauthorizedCitations, citation.Source)
			}
		}
	}

	result.Passed = len(result.MissingFacts) == 0 &&
		len(result.ForbiddenFacts) == 0 &&
		!result.MissingCitation &&
		len(result.UnauthorizedCitations) == 0 &&
		result.NoAnswerViolation == ""
	return result
}
