package governance

import "eino-quickstart/internal/rag/domain"

type Hasher interface {
	HashContent(content string) string
	HashMetadata(metadata domain.Metadata) string
}
