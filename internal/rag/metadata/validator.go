package metadata

import (
	"eino-quickstart/internal/rag/domain"
	"fmt"
	"strconv"
)

type Validator struct {
	registry Registry
}

func NewValidator(registry Registry) *Validator {
	return &Validator{
		registry: registry,
	}
}

func (v *Validator) Validate(
	schemaName string,
	version int,
	values domain.Metadata,
) error {
	schema, ok := v.registry.Get(schemaName, version)
	if !ok {
		return fmt.Errorf("%w: schema %s v%d not found", domain.ErrMetadataValidation, schemaName, version)
	}

	for _, field := range schema.Fields {
		value, exists := values[field.Name]

		if field.Required && !exists {
			return fmt.Errorf("%w: required field %q missing", domain.ErrMetadataValidation, field.Name)
		}

		if !exists {
			continue
		}

		if err := validateType(field.Type, value); err != nil {
			return fmt.Errorf("%w: field %q: %v", domain.ErrMetadataValidation, field.Name, err)
		}
	}

	return nil
}

func validateType(fieldType FieldType, value any) error {
	switch fieldType {
	case FieldTypeString:
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string")
		}

	case FieldTypeInteger:
		switch value.(type) {
		case int, int8, int16, int32, int64:
		default:
			return fmt.Errorf("expected integer")
		}

	case FieldTypeFloat:
		switch value.(type) {
		case float32, float64:
		default:
			return fmt.Errorf("expected float")
		}

	case FieldTypeBoolean:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean")
		}

	case FieldTypeStringArray:
		values, ok := value.([]string)
		if !ok {
			return fmt.Errorf("expected []string")
		}

		for _, item := range values {
			if item == "" {
				return fmt.Errorf("array contains empty string")
			}
		}

	case FieldTypeNumberArray:
		switch value.(type) {
		case []int, []int64, []float32, []float64:
		default:
			return fmt.Errorf("expected number array")
		}

	default:
		return fmt.Errorf(
			"unsupported field type %q",
			strconv.Quote(string(fieldType)),
		)
	}

	return nil
}
