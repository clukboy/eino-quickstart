package product

import "strings"

type Product struct {
	Model         string
	ExactModel    string
	Category      string
	Subcategory   string
	Series        string
	Name          string
	Installation  string
	DoorMaterial  string
	ForceType     string
	BaseMaterial  string
	FamilyPrefix  string
	VariantModels []string
}

func (p Product) SearchText() string {
	return "型号：" + p.Model +
		"\n精确型号：" + p.ExactModel +
		"\n大类：" + p.Category +
		"\n细分品类：" + p.Subcategory +
		"\n产品系列：" + p.Series +
		"\n产品名称：" + p.Name +
		"\n安装方式：" + p.Installation +
		"\n门板材质：" + p.DoorMaterial +
		"\n力学类型：" + p.ForceType +
		"\n基材材质：" + p.BaseMaterial +
		"\n家族前缀：" + p.FamilyPrefix +
		"\n变体型号：" + strings.Join(p.VariantModels, "、")
}

func (p Product) ToMap() map[string]any {
	return map[string]any{
		"model":          p.Model,
		"name":           p.Name,
		"category":       p.Category,
		"subcategory":    p.Subcategory,
		"series":         p.Series,
		"installation":   p.Installation,
		"door_material":  p.DoorMaterial,
		"force_type":     p.ForceType,
		"base_material":  p.BaseMaterial,
		"family_prefix":  p.FamilyPrefix,
		"variant_models": p.VariantModels,
		"exact_model":    p.ExactModel,
	}
}
