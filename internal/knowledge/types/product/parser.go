package product

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	fieldLinePattern = regexp.MustCompile(
		`^\s*(?:\d+\.\s*)?([^:：]+)\s*[:：]\s*(.*?)\s*$`,
	)

	inlineMarkdownLinkPattern = regexp.MustCompile(
		`\[([^\]]+)\]\([^)]+\)`,
	)
)

// Parse 从产品 Markdown 中解析结构化产品字段。
//
// 支持：
//
// 1. 型号: H11
// 2. 精确型号: H11
// 3. 大类: 铰链
//
// 同时支持：
//
// 型号：H11
//
// 第一版故意采用简单规则，避免引入 LLM 解析导致产品索引结果不稳定。
func Parse(content string) (Product, error) {
	content = strings.TrimSpace(content)

	if content == "" {
		return Product{}, fmt.Errorf("product content is empty")
	}

	var p Product

	lines := strings.Split(content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}

		matches := fieldLinePattern.FindStringSubmatch(line)
		if len(matches) != 3 {
			continue
		}

		fieldName := normalizeFieldName(matches[1])
		value := normalizeFieldValue(matches[2])

		if value == "" {
			continue
		}

		switch fieldName {
		case "型号":
			p.Model = value

		case "精确型号":
			p.ExactModel = value

		case "大类":
			p.Category = value

		case "细分品类":
			p.Subcategory = value

		case "产品系列":
			p.Series = value

		case "产品名称":
			p.Name = value

		case "安装方式":
			p.Installation = value

		case "门板材质":
			p.DoorMaterial = value

		case "力学类型":
			p.ForceType = value

		case "基材材质":
			p.BaseMaterial = value

		case "家族前缀":
			p.FamilyPrefix = value

		case "同族所有变体型号列表":
			p.VariantModels = splitVariants(value)
		}
	}

	if p.ExactModel == "" {
		p.ExactModel = p.Model
	}

	if p.FamilyPrefix == "" {
		p.FamilyPrefix = p.Model
	}

	if p.Model == "" {
		return Product{}, fmt.Errorf("product model is required")
	}

	return p, nil
}

func TryParse(content string) (*Product, error) {
	content = strings.TrimSpace(content)

	if content == "" {
		return nil, nil
	}

	productInfo, err := Parse(content)
	if err != nil {
		if strings.Contains(err.Error(), "product model is required") {
			return nil, nil
		}

		return nil, err
	}

	return &productInfo, nil
}

func normalizeFieldName(value string) string {
	value = strings.TrimSpace(value)

	// 去掉 Markdown 粗体标记。
	value = strings.Trim(value, "*")

	return value
}

func normalizeFieldValue(value string) string {
	value = strings.TrimSpace(value)

	// Markdown emphasis
	value = strings.ReplaceAll(value, "**", "")
	value = strings.ReplaceAll(value, "__", "")

	// Markdown inline link
	value = inlineMarkdownLinkPattern.ReplaceAllString(
		value,
		`$1`,
	)

	return strings.TrimSpace(value)
}

func splitVariants(value string) []string {
	value = strings.TrimSpace(value)

	if value == "" {
		return nil
	}

	separators := []string{
		"，",
		",",
		"、",
		";",
		"；",
		"/",
		"|",
	}

	for _, separator := range separators {
		value = strings.ReplaceAll(value, separator, " ")
	}

	parts := strings.Fields(value)

	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{})

	for _, item := range parts {
		item = strings.TrimSpace(item)

		if item == "" {
			continue
		}

		if _, exists := seen[item]; exists {
			continue
		}

		seen[item] = struct{}{}
		result = append(result, item)
	}

	return result
}
