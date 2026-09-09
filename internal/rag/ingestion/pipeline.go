package ingestion

import (
	"context"

	"eino-quickstart/internal/rag/domain"
)

type Pipeline struct {
	Loader       Loader
	Parser       Parser
	Transformers []Transformer
	Chunker      Chunker
	Enrichers    []Enricher
}

func (p *Pipeline) Process(ctx context.Context, source domain.Source) ([]*domain.Chunk, error) {

	documents, err := p.Loader.Load(ctx, source)
	if err != nil {
		return nil, err
	}

	if p.Parser != nil {
		documents, err = p.Parser.Parse(ctx, documents)
		if err != nil {
			return nil, err
		}
	}

	for _, transformer := range p.Transformers {
		documents, err = transformer.Transform(ctx, documents)
		if err != nil {
			return nil, err
		}
	}

	var chunks []*domain.Chunk

	for _, document := range documents {
		items, err := p.Chunker.Split(ctx, document)
		if err != nil {
			return nil, err
		}

		for _, enricher := range p.Enrichers {
			if err := enricher.Enrich(ctx, document, items); err != nil {
				return nil, err
			}
		}

		chunks = append(chunks, items...)
	}

	return chunks, nil
}
