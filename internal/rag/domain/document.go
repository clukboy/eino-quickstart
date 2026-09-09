package domain

import "time"

type DocumentStatus string

const (
	DocumentStatusPending  DocumentStatus = "pending"
	DocumentStatusIndexing DocumentStatus = "indexing"
	DocumentStatusReady    DocumentStatus = "ready"
	DocumentStatusFailed   DocumentStatus = "failed"
	DocumentStatusDeleted  DocumentStatus = "deleted"
)

// Document is the generic RAG document.
type Document struct {
	ID          string
	KnowledgeID string

	Title   string
	Content string

	Source   Source
	Metadata Metadata

	ContentHash string
	Version     int

	Status DocumentStatus

	CreatedAt time.Time
	UpdatedAt time.Time
}
