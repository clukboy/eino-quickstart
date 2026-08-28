package embedding

import (
	"context"
	"fmt"
)

type FakeEmbedder struct {
	Vector []float32
}

func (f FakeEmbedder) Dimensions() int {
	return len(f.Vector)
}

func (f FakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if len(f.Vector) == 0 {
		return nil, fmt.Errorf("fake embedding vector is empty")
	}

	embeddings := make([][]float32, len(texts))
	for i := range texts {
		embeddings[i] = append([]float32(nil), f.Vector...)
	}
	return embeddings, nil
}
