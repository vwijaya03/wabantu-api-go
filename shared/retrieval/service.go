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
	Tenant   TenantIdentity
	Query    string
	TopK     int
	Mode     RetrievalMode
	MinScore float64 // minimum RRF score for vector mode FAQ direct
}

// RetrieveKBResult holds fused hits and diagnostics.
type RetrieveKBResult struct {
	Entries     []ScoredEntry
	VectorHits  []Hit
	LexicalHits []ScoredEntry
	UsedVector  bool
	ShadowOnly  bool
}

// Service wires embedder + vector store + resilience.
type Service struct {
	Embedder Embedder
	Store    VectorStore
	Breaker  *CircuitBreaker
	Budget   *Budget
}

func NewService(embedder Embedder, store VectorStore) *Service {
	return &Service{
		Embedder: embedder,
		Store:    store,
		Breaker:  NewCircuitBreaker(5, 30*time.Second),
		Budget:   NewBudget(8),
	}
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

	if s.Breaker != nil && !s.Breaker.Allow() {
		res.Entries = lexicalHits
		return res, nil
	}

	var vectorHits []Hit
	err = WithBudget(ctx, s.Budget, func() error {
		vecs, e := s.Embedder.Embed(ctx, []string{req.Query})
		if e != nil {
			return e
		}
		if len(vecs) == 0 {
			return fmt.Errorf("empty query embedding")
		}
		vectorHits, e = s.Store.Query(ctx, ns, vecs[0], req.TopK, PineconeFilterActiveKB())
		return e
	})
	if err != nil {
		if s.Breaker != nil {
			s.Breaker.RecordFailure(err)
		}
		res.Entries = lexicalHits
		return res, nil
	}
	if s.Breaker != nil {
		s.Breaker.RecordSuccess()
	}

	res.VectorHits = vectorHits
	res.UsedVector = true
	res.ShadowOnly = req.Mode == ModeShadow

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
	recs, err := BuildKBVectorRecords(entryID, version, category, chunks, vectors)
	if err != nil {
		return err
	}
	return s.Store.Upsert(ctx, ns, recs)
}

// DeleteKB removes all vectors for a KB entry (by prefix ids for v1 single chunk).
func (s *Service) DeleteKB(ctx context.Context, tenant TenantIdentity, entryID string, version int64) error {
	if s == nil || s.Store == nil {
		return nil
	}
	ns, err := Namespace(tenant)
	if err != nil {
		return err
	}
	// v1: single chunk at index 0; delete by id
	id := KBVectorID(entryID, version, 0)
	return s.Store.DeleteIDs(ctx, ns, []string{id})
}

// DeleteKBAllVersions deletes vectors for entry across known versions (best-effort ids 0..maxVer).
func (s *Service) DeleteKBAllVersions(ctx context.Context, tenant TenantIdentity, entryID string, maxVersion int64) error {
	if maxVersion < 0 {
		maxVersion = 0
	}
	ids := make([]string, 0, maxVersion+1)
	for v := int64(0); v <= maxVersion; v++ {
		ids = append(ids, KBVectorID(entryID, v, 0))
	}
	if s == nil || s.Store == nil {
		return nil
	}
	ns, err := Namespace(tenant)
	if err != nil {
		return err
	}
	return s.Store.DeleteIDs(ctx, ns, ids)
}
