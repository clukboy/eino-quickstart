package knowledge

import (
	"strings"
	"testing"
)

func TestNormalizeMarkdown(t *testing.T) {
	input := `
# H11 二段力滑入式铰链

**型号**：**H11**

**产品系列**：**经典系列**

- 产品名称：**二段力滑入式铰链**
- 安装方式：**滑入式**
- 力学类型：**二段力**

[产品详情](https://example.com)

![产品图片](https://example.com/h11.png)
`

	got := NormalizeMarkdown(input)

	if got == "" {
		t.Fatal("normalized content is empty")
	}

	invalidTokens := []string{
		"**",
		"##",
		"###",
	}

	for _, token := range invalidTokens {
		if strings.Contains(got, token) {
			t.Errorf(
				"normalized content contains %q: %q",
				token,
				got,
			)
		}
	}

	required := []string{
		"H11",
		"经典系列",
		"二段力滑入式铰链",
		"滑入式",
		"二段力",
		"产品详情",
		"产品图片",
	}

	for _, value := range required {
		if !strings.Contains(got, value) {
			t.Errorf(
				"normalized content missing %q: %q",
				value,
				got,
			)
		}
	}
}
