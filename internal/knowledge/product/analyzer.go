package product

import (
	"eino-quickstart/internal/rag/domain"
	"regexp"
	"strings"
)

type Analyzer struct {
	modelPattern *regexp.Regexp
}

func NewAnalyzer() *Analyzer {
	return &Analyzer{
		modelPattern: regexp.MustCompile(`(?i)\b[A-Z]{1,6}\d{1,5}[A-Z0-9-]*\b`),
	}
}

func (a *Analyzer) Analyze(query string) domain.QuerySignals {
	signals := domain.QuerySignals{}

	signals.Language = detectLanguage(query)

	signals.Terms = extractTerms(query)

	matches := a.modelPattern.FindAllString(query, -1)

	for _, match := range matches {
		value := strings.ToUpper(strings.TrimSpace(match))

		signals.Entities = append(signals.Entities,
			domain.Entity{
				Type:  "product_model",
				Value: value,
				Score: 1,
			},
		)
	}

	return signals
}

func extractTerms(query string) []string {
	fields := strings.Fields(query)

	result := make([]string, 0, len(fields))

	for _, field := range fields {
		field = strings.TrimSpace(field)

		if field == "" {
			continue
		}

		result = append(result, field)
	}

	return result
}

func detectLanguage(query string) string {
	for _, r := range query {
		if r >= 0x4E00 && r <= 0x9FFF {
			return "zh"
		}
	}

	return "en"
}
