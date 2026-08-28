package knowledge

import (
	"context"
	"eino-quickstart/ent"
)

type Service struct {
	Client          *ent.Client
	Chunker         Chunker
	MaxChunksPerDoc int
	EmbeddingModel  string
}

func (s *Service) Ingest(
	ctx context.Context,
	source string,
	title string,
	content string,
	ownerSubject string,
	visibility string,
) error {

	return nil
}
