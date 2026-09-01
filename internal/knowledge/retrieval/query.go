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

	match := modelPattern.FindString(query)
	if match == "" {
		return info
	}

	info.Model = strings.ToUpper(strings.TrimSpace(match))
	info.HasModel = true

	return info
}
