package ai

import (
	"context"
	"strings"
	"time"

	"encore.dev/rlog"

	appflag "encore.app/wabantu/flag"
	bf "encore.app/wabantu/internal/buyerflow"
	"encore.app/wabantu/shared/retrieval"
)

const retrievalBudget = 400 * time.Millisecond

// retrieveKBHybrid runs vector+lexical RRF when retrieval_mode allows; falls back to lexical.
func (s *AutoReplyService) retrieveKBHybrid(
	ctx context.Context,
	tenantID, tenantSchema, query string,
	kb []dbKBEntry,
) ([]dbKBEntry, float64, *retrieval.RetrieveKBResult) {
	lexical := retrieveHybridKB(query, kb)
	mode := appflag.RetrievalMode(ctx, tenantID)

	svc := retrieval.DefaultService()
	if svc == nil || mode == retrieval.ModeDisabled {
		return lexical, topKBMatchScore(query, kb), nil
	}

	ctx, cancel := context.WithTimeout(ctx, retrievalBudget)
	defer cancel()

	tenant := retrieval.TenantIdentity{TenantID: tenantID, TenantSchema: tenantSchema}
	lexicalRanker := func(_ context.Context, q string, topK int) ([]retrieval.ScoredEntry, error) {
		return lexicalRankedEntries(q, kb, topK), nil
	}

	res, err := svc.RetrieveKB(ctx, retrieval.RetrieveKBRequest{
		Tenant: tenant,
		Query:  query,
		TopK:   20,
		Mode:   mode,
	}, lexicalRanker)
	fallback := err != nil || (res != nil && res.LexicalFallback)
	if err != nil || res == nil {
		retrieval.LogQuery(ctx, "kb", tenantID, string(mode), true, false)
		recordRetrievalQueryMetrics("kb", string(mode), true, false, retrieval.LatencyP95Ms())
		rlog.Warn("retrieval KB failed, lexical fallback", "err", err, "tenant", tenantID)
		return lexical, topKBMatchScore(query, kb), res
	}

	if fallback {
		retrieval.LogQuery(ctx, "kb", tenantID, string(mode), true, len(res.Entries) == 0)
		recordRetrievalQueryMetrics("kb", string(mode), true, len(res.Entries) == 0, retrieval.LatencyP95Ms())
	}

	if mode == retrieval.ModeShadow {
		rlog.Info("retrieval shadow",
			"tenant", tenantID,
			"vectorHits", len(res.VectorHits),
			"fused", len(res.Entries),
			"lexical", len(res.LexicalHits),
			"fallback", fallback,
		)
		if fallback {
			return lexical, topKBMatchScore(query, kb), res
		}
		zero := len(res.Entries) == 0
		retrieval.LogQuery(ctx, "kb", tenantID, string(mode), false, zero)
		recordRetrievalQueryMetrics("kb", string(mode), false, zero, retrieval.LatencyP95Ms())
		return lexical, topKBMatchScore(query, kb), res
	}

	if fallback {
		return lexical, topKBMatchScore(query, kb), res
	}

	ordered := orderKBByScoredEntries(kb, res.Entries)
	topScore := topKBMatchScore(query, kb)
	if len(res.Entries) > 0 {
		topScore = res.Entries[0].Score
	}
	if len(ordered) == 0 {
		ordered = lexical
	}
	zero := len(res.Entries) == 0
	retrieval.LogQuery(ctx, "kb", tenantID, string(mode), false, zero)
	recordRetrievalQueryMetrics("kb", string(mode), false, zero, retrieval.LatencyP95Ms())
	return ordered, topScore, res
}

func lexicalRankedEntries(query string, kb []dbKBEntry, topK int) []retrieval.ScoredEntry {
	ordered := retrieveHybridKB(query, kb)
	out := make([]retrieval.ScoredEntry, 0, len(ordered))
	byID := map[string]dbKBEntry{}
	for _, e := range kb {
		byID[e.ID] = e
	}
	for i, e := range ordered {
		if i >= topK {
			break
		}
		score := 1.0 / float64(i+1)
		out = append(out, retrieval.ScoredEntry{EntryID: e.ID, Score: score, Source: retrieval.SourceKB})
	}
	return out
}

func orderKBByScoredEntries(kb []dbKBEntry, scores []retrieval.ScoredEntry) []dbKBEntry {
	byID := map[string]dbKBEntry{}
	for _, e := range kb {
		byID[e.ID] = e
	}
	out := make([]dbKBEntry, 0, len(scores))
	for _, s := range scores {
		if e, ok := byID[s.EntryID]; ok {
			out = append(out, e)
		}
	}
	return out
}

func tryFAQDirectAnswerHybrid(
	query string,
	kb []dbKBEntry,
	rrfScores []retrieval.ScoredEntry,
	mode retrieval.RetrievalMode,
) (string, bool) {
	if mode == retrieval.ModeVector && len(rrfScores) > 0 && bf.FAQDirectGuardsPass(query) {
		top, ok := retrieval.FAQDirectOK(rrfScores, retrieval.DefaultFAQMinScore, retrieval.DefaultFAQMinMargin)
		if ok {
			for _, e := range kb {
				if e.ID == top.EntryID {
					ans := strings.TrimSpace(e.Answer)
					if ans != "" {
						return ans, true
					}
				}
			}
		}
	}
	return tryFAQDirectAnswer(query, kb)
}

func (s *AutoReplyService) replyFromBusinessCatalogHybrid(
	ctx context.Context,
	tenantID, tenantSchema, userText string,
	profile *dbBusinessProfile,
	catalog []dbCatalogItem,
	history []dbMessage,
) (string, bool) {
	mode := appflag.RetrievalMode(ctx, tenantID)
	if mode != retrieval.ModeVector {
		return replyFromBusinessCatalog(userText, profile, catalog, history)
	}
	svc := retrieval.DefaultService()
	if svc == nil {
		return replyFromBusinessCatalog(userText, profile, catalog, history)
	}
	qctx, cancel := context.WithTimeout(ctx, retrievalBudget)
	defer cancel()
	tenant := retrieval.TenantIdentity{TenantID: tenantID, TenantSchema: tenantSchema}
	hits, err := svc.RetrieveCatalogCandidates(qctx, tenant, userText, 3)
	fallback := err != nil || len(hits) == 0
	if fallback {
		retrieval.LogQuery(ctx, "catalog", tenantID, string(mode), err != nil, len(hits) == 0)
		recordRetrievalQueryMetrics("catalog", string(mode), err != nil, len(hits) == 0, retrieval.LatencyP95Ms())
		return replyFromBusinessCatalog(userText, profile, catalog, history)
	}
	retrieval.LogQuery(ctx, "catalog", tenantID, string(mode), false, false)
	recordRetrievalQueryMetrics("catalog", string(mode), false, false, retrieval.LatencyP95Ms())
	vctx := &bf.CatalogVectorContext{Hits: hits}
	return bf.ReplyFromBusinessCatalogVector(userText, profile, catalog, history, vctx)
}
