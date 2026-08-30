package retrieval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// KBVectorID is deterministic: kb:{entry_id}:v{version}:c{chunk_index}.
func KBVectorID(entryID string, version int64, chunkIndex int) string {
	return fmt.Sprintf("kb:%s:v%d:c%d", entryID, version, chunkIndex)
}

// CatalogVectorID is deterministic: catalog:{item_id}:v{version}:c{chunk_index}.
func CatalogVectorID(itemID string, version int64, chunkIndex int) string {
	return fmt.Sprintf("catalog:%s:v%d:c%d", itemID, version, chunkIndex)
}

// ContentHash returns SHA256 hex of normalized text for idempotency checks.
func ContentHash(parts ...string) string {
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			_, _ = h.Write([]byte{0})
		}
		_, _ = h.Write([]byte(strings.TrimSpace(p)))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// KBDocumentText joins question and answer for embedding.
func KBDocumentText(question, answer string) string {
	return strings.TrimSpace(question) + "\n\n" + strings.TrimSpace(answer)
}

// CatalogDocumentText builds embeddable catalog text.
func CatalogDocumentText(name, description, externalCode string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(name))
	if c := strings.TrimSpace(externalCode); c != "" {
		b.WriteString("\nSKU: ")
		b.WriteString(c)
	}
	if d := strings.TrimSpace(description); d != "" {
		b.WriteString("\n")
		b.WriteString(d)
	}
	return b.String()
}

// ChunkKB splits a KB entry into chunks (v1: single chunk).
func ChunkKB(entryID string, version int64, question, answer string) []Chunk {
	text := KBDocumentText(question, answer)
	if text == "" {
		return nil
	}
	return []Chunk{{
		ID:      KBVectorID(entryID, version, 0),
		Index:   0,
		Text:    text,
		Version: version,
	}}
}

// ChunkCatalog splits catalog item into chunks (v1: single chunk).
func ChunkCatalog(itemID string, version int64, name, description, externalCode string) []Chunk {
	text := CatalogDocumentText(name, description, externalCode)
	if text == "" {
		return nil
	}
	return []Chunk{{
		ID:      CatalogVectorID(itemID, version, 0),
		Index:   0,
		Text:    text,
		Version: version,
	}}
}

// BuildKBVectorRecords builds Pinecone records from chunks and embeddings.
func BuildKBVectorRecords(entryID string, version int64, category string, chunks []Chunk, vectors [][]float32) ([]VectorRecord, error) {
	if len(chunks) != len(vectors) {
		return nil, fmt.Errorf("chunks/vectors length mismatch: %d vs %d", len(chunks), len(vectors))
	}
	recs := make([]VectorRecord, len(chunks))
	for i, ch := range chunks {
		meta := map[string]any{
			"source":     string(SourceKB),
			"entry_id":   entryID,
			"version":    version,
			"chunk":      ch.Index,
			"content":    truncateMeta(ch.Text, 1000),
		}
		if category != "" {
			meta["category"] = category
		}
		recs[i] = VectorRecord{
			ID:       ch.ID,
			Values:   vectors[i],
			Metadata: meta,
		}
	}
	return recs, nil
}

func truncateMeta(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// Namespace validates tenant schema for Pinecone namespace.
func Namespace(tenant TenantIdentity) (string, error) {
	ns := strings.TrimSpace(tenant.TenantSchema)
	if ns == "" {
		return "", fmt.Errorf("tenant schema required for retrieval namespace")
	}
	if !strings.HasPrefix(ns, "t_") {
		return "", fmt.Errorf("invalid tenant schema namespace: %s", ns)
	}
	return ns, nil
}

// PineconeFilterActiveKB returns metadata filter for active KB entries.
func PineconeFilterActiveKB() map[string]any {
	return map[string]any{
		"source": map[string]any{"$eq": string(SourceKB)},
	}
}

// EntryIDFromHit extracts entry_id from vector metadata.
func EntryIDFromHit(h Hit) string {
	if h.Metadata == nil {
		return ""
	}
	if v, ok := h.Metadata["entry_id"].(string); ok {
		return v
	}
	return ""
}

// IgnoreContext is a test helper.
var _ = context.Background
