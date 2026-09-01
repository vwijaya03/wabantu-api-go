package retrieval

import (
	"context"
	"sort"
	"sync"
)

// MemoryStore is an in-process VectorStore for tests.
type MemoryStore struct {
	mu     sync.RWMutex
	byNS   map[string]map[string]storedVector
}

type storedVector struct {
	values   []float32
	metadata map[string]any
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byNS: map[string]map[string]storedVector{}}
}

func (m *MemoryStore) Upsert(_ context.Context, namespace string, records []VectorRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byNS[namespace] == nil {
		m.byNS[namespace] = map[string]storedVector{}
	}
	for _, r := range records {
		vals := make([]float32, len(r.Values))
		copy(vals, r.Values)
		meta := map[string]any{}
		for k, v := range r.Metadata {
			meta[k] = v
		}
		m.byNS[namespace][r.ID] = storedVector{values: vals, metadata: meta}
	}
	return nil
}

func (m *MemoryStore) Query(_ context.Context, namespace string, vector []float32, topK int, filter map[string]any) ([]Hit, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ns := m.byNS[namespace]
	if ns == nil {
		return nil, nil
	}
	type scored struct {
		id    string
		score float64
		meta  map[string]any
	}
	var hits []scored
	for id, sv := range ns {
		if !metadataMatch(sv.metadata, filter) {
			continue
		}
		hits = append(hits, scored{id: id, score: cosineSimilarity(vector, sv.values), meta: sv.metadata})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if topK > 0 && len(hits) > topK {
		hits = hits[:topK]
	}
	out := make([]Hit, len(hits))
	for i, h := range hits {
		out[i] = Hit{ID: h.id, Score: h.score, Metadata: h.meta}
	}
	return out, nil
}

func (m *MemoryStore) DeleteIDs(_ context.Context, namespace string, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ns := m.byNS[namespace]
	for _, id := range ids {
		delete(ns, id)
	}
	return nil
}

func (m *MemoryStore) DeleteByFilter(ctx context.Context, namespace string, filter map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ns := m.byNS[namespace]
	for id, sv := range ns {
		if metadataMatch(sv.metadata, filter) {
			delete(ns, id)
		}
	}
	return nil
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (sqrt(na) * sqrt(nb))
}

func sqrt(x float64) float64 {
	// Newton
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 10; i++ {
		z -= (z*z - x) / (2 * z)
	}
	return z
}

func metadataMatch(meta, filter map[string]any) bool {
	if len(filter) == 0 {
		return true
	}
	for k, want := range filter {
		got, ok := meta[k]
		if !ok {
			return false
		}
		if eq, ok := want.(map[string]any); ok {
			if eqVal, ok := eq["$eq"]; ok {
				if got != eqVal {
					return false
				}
				continue
			}
		}
		if got != want {
			return false
		}
	}
	return true
}
