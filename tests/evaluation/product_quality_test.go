package evaluation

import (
	"eino-quickstart/internal/knowledge"
	"eino-quickstart/internal/knowledge/types/product"
	"strings"
	"testing"
)

func TestProductKnowledgeQuality(t *testing.T) {
	content := `
# H11 二段力滑入式铰链

**型号**：**H11**
**精确型号**：**H11**
**大类**：**铰链**
**细分品类**：**缓冲铰链**
**产品系列**：**经典系列**
**产品名称**：**二段力滑入式铰链**
**安装方式**：**滑入式**
**门板材质**：**木门**
**力学类型**：**二段力**
**基材材质**：**冷轧钢**
**家族前缀**：**H11**
**同族所有变体型号列表**：**H11**
`

	parsed, err := product.Parse(content)
	if err != nil {
		t.Fatalf("parse product: %v", err)
	}

	assertNotContainsMarkdown(t, parsed.Model)
	assertNotContainsMarkdown(t, parsed.ExactModel)
	assertNotContainsMarkdown(t, parsed.Name)
	assertNotContainsMarkdown(t, parsed.Installation)

	if parsed.Model != "H11" {
		t.Fatalf("model = %q, want H11", parsed.Model)
	}

	if parsed.ExactModel != "H11" {
		t.Fatalf(
			"exact model = %q, want H11",
			parsed.ExactModel,
		)
	}

	if parsed.Name != "二段力滑入式铰链" {
		t.Fatalf(
			"name = %q, want 二段力滑入式铰链",
			parsed.Name,
		)
	}

	if parsed.Installation != "滑入式" {
		t.Fatalf(
			"installation = %q, want 滑入式",
			parsed.Installation,
		)
	}

	searchText := parsed.SearchText()

	if strings.Contains(searchText, "**") {
		t.Fatalf(
			"SearchText contains markdown marker: %q",
			searchText,
		)
	}

	normalized := knowledge.NormalizeMarkdown(content)

	if strings.Contains(normalized, "**") {
		t.Fatalf(
			"normalized document contains markdown marker: %q",
			normalized,
		)
	}
}

func assertNotContainsMarkdown(t *testing.T, value string) {
	t.Helper()

	if strings.Contains(value, "**") {
		t.Fatalf(
			"value still contains markdown marker: %q",
			value,
		)
	}
}
