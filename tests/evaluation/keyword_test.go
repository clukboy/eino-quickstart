package evaluation

import (
	"eino-quickstart/internal/knowledge"
	"strings"
	"testing"
)

func TestProductKeywordQuality(t *testing.T) {
	doc := `
## H11 二段力滑入式铰链

**H11**

- 产品系列：经典系列
- 产品名称：二段力滑入式铰链
- 安装方式：滑入式
- 力学类型：二段力
`

	normalized := knowledge.NormalizeMarkdown(doc)

	invalidTokens := []string{
		"**",
		"##",
		"###",
		"- ",
		"* ",
	}

	for _, token := range invalidTokens {
		if strings.Contains(normalized, token) {
			t.Errorf(
				"normalized content contains invalid markdown token %q: %q",
				token,
				normalized,
			)
		}
	}

	required := []string{
		"H11",
		"经典系列",
		"二段力滑入式铰链",
		"滑入式",
		"二段力",
	}

	for _, keyword := range required {
		if !strings.Contains(normalized, keyword) {
			t.Errorf("missing keyword %q", keyword)
		}
	}
}
