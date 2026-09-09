package metadata

import (
	"strings"

	"eino-quickstart/internal/rag/domain"
)

type Normalizer interface {
	Normalize(domain.Metadata) domain.Metadata
}

type DefaultNormalizer struct{}

func (DefaultNormalizer) Normalize(input domain.Metadata) domain.Metadata {
	result := make(domain.Metadata, len(input))

	for key, value := range input {
		switch v := value.(type) {
		case string:
			result[key] = strings.TrimSpace(v)
		default:
			result[key] = value
		}
	}
	return result
}
