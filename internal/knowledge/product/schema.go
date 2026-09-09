package product

type Product struct {
	ID string

	Model      string
	ExactModel string

	FamilyID     string
	FamilyPrefix string

	Series      string
	Category    string
	Subcategory string

	Name string

	Aliases []string

	Attributes map[string]any

	Variants []ProductVariant
}

type ProductVariant struct {
	Model string

	Type string

	Attributes map[string]any
}
