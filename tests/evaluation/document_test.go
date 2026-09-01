package evaluation

import (
	"strings"
	"testing"

	"eino-quickstart/internal/knowledge"
)

func TestProductDocumentIntegrity(t *testing.T) {
	content := `
# H11 二段力滑入式铰链

型号：H11

产品系列：经典系列

产品名称：二段力滑入式铰链

安装方式：滑入式

力学类型：二段力
`

	normalized := knowledge.NormalizeMarkdown(content)

	t.Log("========== ORIGINAL ==========")
	t.Log(content)

	t.Log("========== NORMALIZED ==========")
	t.Log(normalized)

	if strings.TrimSpace(content) == "" {
		t.Fatal("original content is empty")
	}

	if strings.TrimSpace(normalized) == "" {
		t.Fatal("normalized content is empty")
	}

	required := []string{
		"H11",
		"经典系列",
		"二段力滑入式铰链",
		"滑入式",
		"二段力",
	}

	for _, item := range required {
		if !strings.Contains(normalized, item) {
			t.Errorf("normalized content missing %q", item)
		}
	}
}
