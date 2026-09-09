package evaluation

import "eino-quickstart/internal/rag/domain"

type TestCase struct {
	ID string

	Query string

	ExpectedChunkIDs    []string
	ExpectedDocumentIDs []string
	ExpectedMetadata    map[string]any
	ExpectedEntities    []domain.Entity
}

type Dataset struct {
	Name    string
	Version string
	Cases   []TestCase
}
