package retrieval

import (
	"context"
	"fmt"
	"time"
)

// LexicalRanker scores KB entries by token overlap (caller provides ranked ids).
type LexicalRanker func(ctx context.Context, query string, topK int) ([]ScoredEntry, error)

// RetrieveKBRequest is input for hybrid KB retrieval.
type RetrieveKBRequest struct {
	Tenant    TenantIdentity
	Query     string
	TopK      int
	Mode      RetrievalMode
	MinScore  float64 // minimum RRF score for vector mode FAQ direct
	ParentCtx context.Context
}

// RetrieveKBResult holds fused hits and diagnostics.
type RetrieveKBResult struct {
	Entries         []ScoredEntry
	VectorHits      []Hit
	LexicalHits     []ScoredEntry
	UsedVector      bool
	ShadowOnly      bool
	LexicalFallback bool // true when vector path skipped/failed and lexical served
	ZeroVectorHits  bool // true when vector path ran but returned no hits above floor
	FallbackReason  FallbackReason
}

// Service wires embedder + vector store + resilience.
type Service struct {
	Embedder Embedder
	Store    VectorStore
	Breaker  *CircuitBreaker // legacy/tests; production uses Breakers
	Breakers *BreakerPool
	Budget   *Budget
}

func NewService(embedder Embedder, store VectorStore) *Service {
	return &Service{
		Embedder: embedder,
		Store:    store,
		Breakers: NewBreakerPool(5, 30*time.Second),
		Budget:   NewBudget(8),
	}
}

func (s *Service) breakerFor(tenantID string) *CircuitBreaker {
	if s == nil {
		return nil
	}
	if s.Breaker != nil {
		return s.Breaker
	}
	if s.Breakers != nil {
		return s.Breakers.For(tenantID)
	}
	return nil
}

// RetrieveKB runs vector + lexical RRF fusion when mode allows.
func (s *Service) RetrieveKB(ctx context.Context, req RetrieveKBRequest, lexical LexicalRanker) (*RetrieveKBResult, error) {
	if req.TopK <= 0 {
		req.TopK = 8
	}
	res := &RetrieveKBResult{}

	var lexicalHits []ScoredEntry
	if lexical != nil {
		var err error
		lexicalHits, err = lexical(ctx, req.Query, req.TopK)
		if err != nil {
			return nil, err
		}
		res.LexicalHits = lexicalHits
	}

	if req.Mode == ModeDisabled || s == nil || s.Embedder == nil || s.Store == nil {
		res.Entries = lexicalHits
		return res, nil
	}

	ns, err := Namespace(req.Tenant)
	if err != nil {
		return nil, err
	}

	runVector := req.Mode == ModeVector || req.Mode == ModeShadow
	if !runVector {
		res.Entries = lexicalHits
		return res, nil
	}

	breaker := s.breakerFor(req.Tenant.TenantID)
	if breaker != nil && !breaker.Allow() {
		res.Entries = lexicalHits
		res.LexicalFallback = runVector
		res.FallbackReason = FallbackReasonCircuitOpen
		return res, nil
	}

	parentCtx := req.ParentCtx
	if parentCtx == nil {
		parentCtx = ctx
	}

	var vectorHits []Hit
	start := time.Now()
	err = WithBudget(ctx, s.Budget, func() error {
		embedStart := time.Now()
		vecs, e := s.Embedder.Embed(ctx, []string{SanitizeForEmbed(req.Query)})
		RecordEmbedLatency(time.Since(embedStart))
		if e != nil {
			return e
		}
		if len(vecs) == 0 {
			return fmt.Errorf("empty query embedding")
		}
		storeStart := time.Now()
		vectorHits, e = s.Store.Query(ctx, ns, vecs[0], req.TopK, PineconeFilterActiveKB())
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
		res.Entries = lexicalHits
		res.LexicalFallback = true
		res.FallbackReason = FallbackReasonFromCategory(re)
		return res, nil
	}
	if breaker != nil {
		breaker.RecordSuccess()
	}

	vectorHits = FilterHitsByScore(vectorHits, VectorMinSimilarity)
	lexicalHits = FilterScoredEntries(lexicalHits, LexicalMinScore)

	res.VectorHits = vectorHits
	res.UsedVector = true
	res.ShadowOnly = req.Mode == ModeShadow
	res.ZeroVectorHits = len(vectorHits) == 0

	lists := []RankedList{HitsToRankedList(vectorHits, SourceKB)}
	if len(lexicalHits) > 0 {
		lists = append(lists, RankedList{Source: SourceKB, Items: lexicalHits})
	}
	fused := ReciprocalRankFusion(lists, defaultRRFK)
	if req.MinScore > 0 {
		filtered := make([]ScoredEntry, 0, len(fused))
		for _, e := range fused {
			if e.Score >= req.MinScore {
				filtered = append(filtered, e)
			}
		}
		fused = filtered
	}
	if len(fused) > req.TopK {
		fused = fused[:req.TopK]
	}

	if req.Mode == ModeShadow {
		res.Entries = lexicalHits
	} else {
		res.Entries = fused
	}
	return res, nil
}

// IndexKB upserts KB entry vectors (idempotent by vector id).
func (s *Service) IndexKB(ctx context.Context, tenant TenantIdentity, entryID, category, question, answer string, version int64) error {
	if s == nil || s.Embedder == nil || s.Store == nil {
		return fmt.Errorf("retrieval service not configured")
	}
	ns, err := Namespace(tenant)
	if err != nil {
		return err
	}
	chunks := ChunkKB(entryID, version, question, answer)
	if len(chunks) == 0 {
		return nil
	}
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Text
	}
	vectors, err := s.Embedder.Embed(ctx, texts)
	if err != nil {
		return err
	}
	if err := ValidateVectors(s.Embedder, vectors); err != nil {
		return err
	}
	recs, err := BuildKBVectorRecords(entryID, version, category, KBContentHash(question, answer), chunks, vectors)
	if err != nil {
		return err
	}
	return s.Store.Upsert(ctx, ns, recs)
}

// DeleteKB removes vectors for one KB entry version (current + legacy id).
func (s *Service) DeleteKB(ctx context.Context, tenant TenantIdentity, entryID string, version int64) error {
	if s == nil || s.Store == nil {
		return nil
	}
	ns, err := Namespace(tenant)
	if err != nil {
		return err
	}
	ids := []string{
		LegacyKBVectorID(entryID, version, 0),
		KBVectorID(entryID, version, 0),
	}
	return s.Store.DeleteIDs(ctx, ns, ids)
}

// DeleteKBAllVersions deletes vectors for entry across known versions (best-effort).
func (s *Service) DeleteKBAllVersions(ctx context.Context, tenant TenantIdentity, entryID string, maxVersion int64) error {
	return DeleteAllKBEntryVectors(ctx, s, tenant, entryID, maxVersion)
}
