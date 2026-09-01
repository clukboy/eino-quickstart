package product

import (
	"strings"
	"testing"
)

func TestParseProduct(t *testing.T) {
	content := `
# H11 二段力滑入式铰链

1. 型号: H11
2. 精确型号: H11
3. 大类: 铰链
4. 细分品类: 缓冲铰链
5. 产品系列: 经典系列
6. 产品名称: 二段力滑入式铰链
7. 安装方式: 滑入式
8. 门板材质: 木门
9. 力学类型: 二段力
10. 基材材质: 冷轧钢
11. 家族前缀: H11
12. 同族所有变体型号列表: H11
`

	product, err := Parse(content)
	if err != nil {
		t.Fatalf("parse product failed: %v", err)
	}

	if product.Model != "H11" {
		t.Fatalf("unexpected model: %q", product.Model)
	}

	if product.ExactModel != "H11" {
		t.Fatalf("unexpected exact model: %q", product.ExactModel)
	}

	if product.Category != "铰链" {
		t.Fatalf("unexpected category: %q", product.Category)
	}

	if product.Subcategory != "缓冲铰链" {
		t.Fatalf("unexpected subcategory: %q", product.Subcategory)
	}

	if product.Series != "经典系列" {
		t.Fatalf("unexpected series: %q", product.Series)
	}

	if product.Name != "二段力滑入式铰链" {
		t.Fatalf("unexpected name: %q", product.Name)
	}

	if product.Installation != "滑入式" {
		t.Fatalf("unexpected installation: %q", product.Installation)
	}

	if product.DoorMaterial != "木门" {
		t.Fatalf("unexpected door material: %q", product.DoorMaterial)
	}

	if product.ForceType != "二段力" {
		t.Fatalf("unexpected force type: %q", product.ForceType)
	}

	if product.BaseMaterial != "冷轧钢" {
		t.Fatalf("unexpected base material: %q", product.BaseMaterial)
	}

	if product.FamilyPrefix != "H11" {
		t.Fatalf("unexpected family prefix: %q", product.FamilyPrefix)
	}

	if len(product.VariantModels) != 1 {
		t.Fatalf(
			"unexpected variant models: %#v",
			product.VariantModels,
		)
	}
}

func TestParseProductWithoutExactModel(t *testing.T) {
	content := `
型号：H105G
大类：铰链
产品系列：经典系列
`

	product, err := Parse(content)
	if err != nil {
		t.Fatalf("parse product failed: %v", err)
	}

	if product.Model != "H105G" {
		t.Fatalf("unexpected model: %q", product.Model)
	}

	if product.ExactModel != "H105G" {
		t.Fatalf(
			"exact model should fallback to model, got %q",
			product.ExactModel,
		)
	}

	if product.FamilyPrefix != "H105G" {
		t.Fatalf(
			"family prefix should fallback to model, got %q",
			product.FamilyPrefix,
		)
	}
}

func TestProductSearchText(t *testing.T) {
	product := Product{
		Model:         "H11",
		ExactModel:    "H11",
		Category:      "铰链",
		Subcategory:   "缓冲铰链",
		Series:        "经典系列",
		Name:          "二段力滑入式铰链",
		Installation:  "滑入式",
		DoorMaterial:  "木门",
		ForceType:     "二段力",
		BaseMaterial:  "冷轧钢",
		FamilyPrefix:  "H11",
		VariantModels: []string{"H11"},
	}

	text := product.SearchText()

	if text == "" {
		t.Fatal("search text should not be empty")
	}

	expected := []string{
		"H11",
		"铰链",
		"缓冲铰链",
		"滑入式",
		"木门",
		"二段力",
	}

	for _, value := range expected {
		if !contains(text, value) {
			t.Fatalf(
				"search text does not contain %q: %s",
				value,
				text,
			)
		}
	}
}

func contains(value, target string) bool {
	return len(value) >= len(target) &&
		containsString(value, target)
}

func containsString(value, target string) bool {
	for i := 0; i+len(target) <= len(value); i++ {
		if value[i:i+len(target)] == target {
			return true
		}
	}

	return false
}

func TestParseMarkdownBoldValue(t *testing.T) {
	content := `
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

	got, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	tests := map[string]string{
		"Model":        got.Model,
		"ExactModel":   got.ExactModel,
		"Category":     got.Category,
		"Subcategory":  got.Subcategory,
		"Series":       got.Series,
		"Name":         got.Name,
		"Installation": got.Installation,
		"DoorMaterial": got.DoorMaterial,
		"ForceType":    got.ForceType,
		"BaseMaterial": got.BaseMaterial,
		"FamilyPrefix": got.FamilyPrefix,
	}

	expected := map[string]string{
		"Model":        "H11",
		"ExactModel":   "H11",
		"Category":     "铰链",
		"Subcategory":  "缓冲铰链",
		"Series":       "经典系列",
		"Name":         "二段力滑入式铰链",
		"Installation": "滑入式",
		"DoorMaterial": "木门",
		"ForceType":    "二段力",
		"BaseMaterial": "冷轧钢",
		"FamilyPrefix": "H11",
	}

	for field, actual := range tests {
		want := expected[field]

		if actual != want {
			t.Errorf(
				"%s = %q, want %q",
				field,
				actual,
				want,
			)
		}

		if strings.Contains(actual, "**") {
			t.Errorf(
				"%s still contains markdown bold marker: %q",
				field,
				actual,
			)
		}
	}

	if len(got.VariantModels) != 1 {
		t.Fatalf(
			"VariantModels = %#v, want one item",
			got.VariantModels,
		)
	}

	if got.VariantModels[0] != "H11" {
		t.Fatalf(
			"VariantModels[0] = %q, want H11",
			got.VariantModels[0],
		)
	}
}
