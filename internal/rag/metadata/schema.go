package metadata

type FieldType string

const (
	FieldTypeString      FieldType = "string"
	FieldTypeInteger     FieldType = "integer"
	FieldTypeFloat       FieldType = "float"
	FieldTypeBoolean     FieldType = "boolean"
	FieldTypeStringArray FieldType = "string_array"
	FieldTypeNumberArray FieldType = "number_array"
)

type Field struct {
	Name string
	Type FieldType

	Searchable bool
	Filterable bool
	Facetable  bool

	Required bool

	Description string
}

type Schema struct {
	Name    string
	Version int

	Fields []Field
}
