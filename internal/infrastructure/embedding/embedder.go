package embedding

import "context"

type Embedder interface {
	Dimensions() int
	Embed(ctx context.Context, tests []string) ([][]float32, error)
}
