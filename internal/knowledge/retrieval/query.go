package retrieval

import (
	"regexp"
	"strings"
)

type QueryInfo struct {
	Raw      string
	Model    string
	HasModel bool
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
	if len(match) < 2 {
		return info
	}

	info.Model = strings.ToUpper(match[1])
	info.HasModel = true

	return info
}
