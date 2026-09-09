package product

import "strings"

type Normalizer struct{}

func NewNormalizer() *Normalizer {
	return &Normalizer{}
}

func (n *Normalizer) Normalize(product *Product) *Product {
	if product == nil {
		return nil
	}

	product.Model = normalize(product.Model)
	product.ExactModel = normalize(product.ExactModel)

	product.FamilyPrefix = normalize(product.FamilyPrefix)

	product.Series = strings.TrimSpace(product.Series)
	product.Category = strings.TrimSpace(product.Category)
	product.Subcategory = strings.TrimSpace(product.Subcategory)
	product.Name = strings.TrimSpace(product.Name)

	return product
}

func normalize(value string) string {
	return strings.ToUpper(
		strings.TrimSpace(value),
	)
}
