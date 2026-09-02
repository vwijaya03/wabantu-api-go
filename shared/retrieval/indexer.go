package retrieval

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// KBIndexInput is the only allowed KB indexing payload (no generic text).
type KBIndexInput struct {
	Tenant   TenantIdentity
	EntryID  string
	Question string
	Answer   string
	Category string
	Version  int64
	Hash     string
}

// IndexKBEntry upserts vectors and is safe to retry (idempotent vector IDs).
func IndexKBEntry(ctx context.Context, svc *Service, in KBIndexInput) error {
	if svc == nil {
		return fmt.Errorf("retrieval service not configured")
	}
	if in.Version < 1 {
		return fmt.Errorf("invalid embedding version")
	}
	return svc.IndexKB(ctx, in.Tenant, in.EntryID, in.Category, in.Question, in.Answer, in.Version)
}

// DeleteKBEntryVectors removes vectors for the given version.
func DeleteKBEntryVectors(ctx context.Context, svc *Service, tenant TenantIdentity, entryID string, version int64) error {
	if svc == nil {
		return nil
	}
	return svc.DeleteKB(ctx, tenant, entryID, version)
}

// CatalogIndexInput is the only allowed catalog indexing payload.
type CatalogIndexInput struct {
	Tenant       TenantIdentity
	ItemID       string
	Name         string
	Description  string
	ExternalCode string
	Version      int64
	Hash         string
}

// IndexCatalogItem upserts catalog vectors.
func IndexCatalogItem(ctx context.Context, svc *Service, in CatalogIndexInput) error {
	if svc == nil {
		return fmt.Errorf("retrieval service not configured")
	}
	ns, err := Namespace(in.Tenant)
	if err != nil {
		return err
	}
	chunks := ChunkCatalog(in.ItemID, in.Version, in.Name, in.Description, in.ExternalCode)
	if len(chunks) == 0 {
		return nil
	}
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Text
	}
	vectors, err := svc.Embedder.Embed(ctx, texts)
	if err != nil {
		return err
	}
	if err := ValidateVectors(svc.Embedder, vectors); err != nil {
		return err
	}
	recs := make([]VectorRecord, len(chunks))
	for i, ch := range chunks {
		meta := map[string]any{
			"source":          string(SourceCatalog),
			"entry_id":        in.ItemID,
			"version":         in.Version,
			"chunk":           ch.Index,
			"embedding_model": EmbeddingModel,
			"name":            truncateMeta(in.Name, 200),
		}
		if in.ExternalCode != "" {
			meta["external_code"] = in.ExternalCode
		}
		if in.Hash != "" {
			meta["content_hash"] = in.Hash
		}
		recs[i] = VectorRecord{ID: ch.ID, Values: vectors[i], Metadata: meta}
	}
	return svc.Store.Upsert(ctx, ns, recs)
}

// RetrieveCatalogCandidates returns top semantic catalog hits (rules applied by caller).
func (s *Service) RetrieveCatalogCandidates(parentCtx context.Context, ctx context.Context, tenant TenantIdentity, query string, topK int) ([]Hit, error) {
	if s == nil || s.Embedder == nil || s.Store == nil {
		return nil, nil
	}
	if parentCtx == nil {
		parentCtx = ctx
	}
	if topK <= 0 {
		topK = 3
	}
	ns, err := Namespace(tenant)
	if err != nil {
		return nil, err
	}
	breaker := s.breakerFor(tenant.TenantID)
	if breaker != nil && !breaker.Allow() {
		return nil, ErrCircuitOpen
	}
	var hits []Hit
	start := time.Now()
	err = WithBudget(ctx, s.Budget, func() error {
		embedStart := time.Now()
		vecs, e := s.Embedder.Embed(ctx, []string{SanitizeForEmbed(query)})
		RecordEmbedLatency(time.Since(embedStart))
		if e != nil {
			return e
		}
		if len(vecs) == 0 {
			return fmt.Errorf("empty query embedding")
		}
		storeStart := time.Now()
		filter := PineconeFilterActiveCatalog()
		hits, e = s.Store.Query(ctx, ns, vecs[0], topK, filter)
		RecordStoreLatency(time.Since(storeStart))
		return e
	})
	RecordQueryLatency(time.Since(start))
	if err != nil {
		re := ClassifyProviderError(parentCtx, err, providerForStep(err))
		RecordErrorCategory(re.Category, re.Provider)
		if breaker != nil && re.TripsBreaker {
			breaker.RecordFailure(err)
		}
		return nil, err
	}
	if breaker != nil {
		breaker.RecordSuccess()
	}
	return FilterHitsByScore(hits, VectorMinSimilarity), nil
}

func providerForStep(err error) Provider {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "pinecone") {
		return ProviderPinecone
	}
	return ProviderOpenAI
}
