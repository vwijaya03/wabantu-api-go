package retrieval

import (
	"context"
	"hash/fnv"
	"math"
)

// MockEmbedder produces deterministic 1536-dim vectors for tests (no network).
type MockEmbedder struct{}

func NewMockEmbedder() *MockEmbedder { return &MockEmbedder{} }

func (m *MockEmbedder) Dimensions() int { return EmbeddingDims }

func (m *MockEmbedder) Model() string { return "mock-" + EmbeddingModel }

func (m *MockEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = hashToVector(t, EmbeddingDims)
	}
	return out, nil
}

func hashToVector(text string, dims int) []float32 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	seed := h.Sum64()
	vec := make([]float32, dims)
	var state uint64 = seed
	for i := range vec {
		state = state*6364136223846793005 + 1
		u := float64(state>>11) / float64(1<<53)
		vec[i] = float32(u*2 - 1)
	}
	normalize(vec)
	return vec
}

func normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return
	}
	inv := float32(1 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
}
