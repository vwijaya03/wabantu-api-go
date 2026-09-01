package retrieval

// Embedding model constants (OpenAI text-embedding-3-small).
const (
	EmbeddingModel = "text-embedding-3-small"
	EmbeddingDims  = 1536
)

// Source identifies indexed content type in Pinecone metadata.
type Source string

const (
	SourceKB      Source = "kb"
	SourceCatalog Source = "catalog"
)

// RetrievalMode controls vector retrieval rollout per tenant.
type RetrievalMode string

const (
	ModeDisabled RetrievalMode = "disabled"
	ModeShadow   RetrievalMode = "shadow"
	ModeVector   RetrievalMode = "vector"
)

// TenantIdentity is the server-side tenant scope for retrieval (never from client input).
type TenantIdentity struct {
	TenantID     string
	TenantSchema string // Pinecone namespace, e.g. t_acme
}

// Chunk is one embeddable text unit.
type Chunk struct {
	ID      string
	Index   int
	Text    string
	Version int64
}

// VectorRecord is one upsert row for the vector store.
type VectorRecord struct {
	ID       string
	Values   []float32
	Metadata map[string]any
}

// Hit is one retrieval result.
type Hit struct {
	ID       string
	Score    float64
	Metadata map[string]any
}

// ScoredEntry pairs an entity id with a retrieval score (for RRF / FAQ direct).
type ScoredEntry struct {
	EntryID string
	Score   float64
	Source  Source
}
