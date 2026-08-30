package retrieval

import (
	"context"
	"fmt"
)

// Embedder converts text to dense vectors.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dimensions() int
	Model() string
}

// ValidateVectors ensures every vector matches the embedder dimension.
func ValidateVectors(embedder Embedder, vectors [][]float32) error {
	if embedder == nil {
		return fmt.Errorf("embedder is nil")
	}
	want := embedder.Dimensions()
	for i, v := range vectors {
		if len(v) != want {
			return fmt.Errorf("vector %d: got %d dims, want %d", i, len(v), want)
		}
	}
	return nil
}
