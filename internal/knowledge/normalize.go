package knowledge

import (
	"regexp"
	"strings"
)

var (
	markdownBoldPattern    = regexp.MustCompile(`\*\*(.*?)\*\*`)
	markdownItalicPattern  = regexp.MustCompile(`__(.*?)__`)
	markdownHeadingPattern = regexp.MustCompile(`(?m)^\s*#{1,6}\s*`)
	markdownListPattern    = regexp.MustCompile(`(?m)^\s*[-*+]\s+`)
	markdownLinkPattern    = regexp.MustCompile(`\[([^]]+)]\([^)]+\)`)
	markdownImagePattern   = regexp.MustCompile(`!\[([^]]*)]\([^)]+\)`)
	multipleSpacePattern   = regexp.MustCompile(`[ \t]+`)
	multipleNewlinePattern = regexp.MustCompile(`\n{3,}`)
)

// NormalizeMarkdown converts Markdown into normalized plain text.
//
// This function is intended for keyword extraction and retrieval,
// not for replacing the original document content.
func NormalizeMarkdown(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	// 图片必须先处理，否则普通链接规则会匹配到图片。
	content = markdownImagePattern.ReplaceAllString(content, `$1`)

	// Markdown links.
	content = markdownLinkPattern.ReplaceAllString(content, `$1`)

	// Bold.
	content = markdownBoldPattern.ReplaceAllString(content, `$1`)

	// Underline-style emphasis.
	content = markdownItalicPattern.ReplaceAllString(content, `$1`)

	// Heading markers.
	content = markdownHeadingPattern.ReplaceAllString(content, "")

	// List markers.
	content = markdownListPattern.ReplaceAllString(content, "")

	// Remaining emphasis markers.
	content = strings.ReplaceAll(content, "**", "")
	content = strings.ReplaceAll(content, "__", "")

	// Normalize spaces.
	content = multipleSpacePattern.ReplaceAllString(content, " ")

	// Normalize blank lines.
	content = multipleNewlinePattern.ReplaceAllString(content, "\n\n")

	return strings.TrimSpace(content)
}
