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
// Vector hits outside the preloaded window are fetched by ID before ordering.
func (s *AutoReplyService) retrieveKBHybrid(
	ctx context.Context,
	tenantID, tenantSchema, query string,
	ts tenantScopedQuerier,
	kb []dbKBEntry,
) ([]dbKBEntry, float64, *retrieval.RetrieveKBResult) {
	logCtx := ctx
	lexical := retrieveHybridKB(query, kb)
	mode := appflag.RetrievalMode(ctx, tenantID)
	modeStr := string(mode)

	svc := retrieval.DefaultService()
	if svc == nil || mode == retrieval.ModeDisabled {
		return lexical, topKBMatchScore(query, kb), nil
	}

	if mode == retrieval.ModeVector || mode == retrieval.ModeShadow {
		if !s.checkTenantEmbedQuota(ctx, tenantID) {
			retrieval.RecordEmbedQuotaRejected()
			rlog.Warn("retrieval embed quota exceeded, lexical fallback",
				"tenant", tenantID, "mode", modeStr, "fallback_reason", "embed_quota")
			return lexical, topKBMatchScore(query, kb), nil
		}
	}

	qctx, cancel := context.WithTimeout(ctx, retrievalBudget)
	defer cancel()

	tenant := retrieval.TenantIdentity{TenantID: tenantID, TenantSchema: tenantSchema}
	lexicalRanker := func(_ context.Context, q string, topK int) ([]retrieval.ScoredEntry, error) {
		return lexicalRankedEntries(q, kb, topK), nil
	}

	res, err := svc.RetrieveKB(qctx, retrieval.RetrieveKBRequest{
		Tenant: tenant,
		Query:  query,
		TopK:   20,
		Mode:   mode,
	}, lexicalRanker)

	if err != nil || res == nil {
		reason := retrieval.FallbackReasonQueryError
		if err != nil {
			reason = retrieval.ClassifyVectorError(err)
		}
		retrieval.LogQueryWithReason(logCtx, "kb", tenantID, modeStr, true, true, reason)
		recordRetrievalQueryMetrics("kb", modeStr, true, true, retrieval.LatencyP95Ms())
		rlog.Warn("retrieval KB failed, lexical fallback",
			"err", err, "tenant", tenantID, "mode", modeStr, "fallback_reason", reason)
		return lexical, topKBMatchScore(query, kb), res
	}

	fallback := res.LexicalFallback
	zero := kbRetrievalZeroResult(res)
	if fallback {
		retrieval.LogQueryWithReason(logCtx, "kb", tenantID, modeStr, true, zero, res.FallbackReason)
		recordRetrievalQueryMetrics("kb", modeStr, true, zero, retrieval.LatencyP95Ms())
		rlog.Warn("retrieval KB lexical fallback",
			"tenant", tenantID,
			"mode", modeStr,
			"fallback_reason", res.FallbackReason,
			"vector_hits", len(res.VectorHits),
			"zero_vector", res.ZeroVectorHits,
		)
	}

	if mode == retrieval.ModeShadow {
		rlog.Info("retrieval shadow",
			"tenant", tenantID,
			"vectorHits", len(res.VectorHits),
			"fused", len(res.Entries),
			"lexical", len(res.LexicalHits),
			"fallback", fallback,
			"zero_vector", res.ZeroVectorHits,
		)
		if !fallback {
			retrieval.LogQueryWithReason(logCtx, "kb", tenantID, modeStr, false, zero, "")
			recordRetrievalQueryMetrics("kb", modeStr, false, zero, retrieval.LatencyP95Ms())
		}
		return lexical, topKBMatchScore(query, kb), res
	}

	if fallback {
		return lexical, topKBMatchScore(query, kb), res
	}

	kbExpanded := kb
	if ts != nil {
		if missing := collectMissingKBEntryIDs(kb, res.Entries, res.VectorHits); len(missing) > 0 {
			fetched, fetchErr := loadKBEntriesByIDs(ctx, ts, missing)
			if fetchErr != nil {
				rlog.Warn("retrieval KB fetch by id failed", "err", fetchErr, "tenant", tenantID, "n", len(missing))
			} else {
				kbExpanded = mergeKBEntries(kb, fetched)
			}
		}
	}

	ordered := orderKBByScoredEntries(kbExpanded, res.Entries)
	topScore := topKBMatchScore(query, kbExpanded)
	if len(res.Entries) > 0 {
		topScore = res.Entries[0].Score
	}
	if len(ordered) == 0 {
		ordered = lexical
	}
	retrieval.LogQueryWithReason(logCtx, "kb", tenantID, modeStr, false, zero, "")
	recordRetrievalQueryMetrics("kb", modeStr, false, zero, retrieval.LatencyP95Ms())
	return ordered, topScore, res
}

func kbRetrievalZeroResult(res *retrieval.RetrieveKBResult) bool {
	if res == nil {
		return false
	}
	if res.ZeroVectorHits {
		return true
	}
	return len(res.Entries) == 0
}

func lexicalRankedEntries(query string, kb []dbKBEntry, topK int) []retrieval.ScoredEntry {
	scored := scoreKBEntries(query, kb)
	out := make([]retrieval.ScoredEntry, 0, topK)
	for i, s := range scored {
		if i >= topK {
			break
		}
		if s.score < retrieval.LexicalMinScore {
			break
		}
		out = append(out, retrieval.ScoredEntry{EntryID: s.entry.ID, Score: s.score, Source: retrieval.SourceKB})
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
	res *retrieval.RetrieveKBResult,
	mode retrieval.RetrievalMode,
) (string, bool) {
	if mode == retrieval.ModeVector && len(rrfScores) > 0 && bf.FAQDirectGuardsPass(query) {
		var vectorHits []retrieval.Hit
		var lexicalHits []retrieval.ScoredEntry
		if res != nil {
			vectorHits = res.VectorHits
			lexicalHits = res.LexicalHits
		}
		top, ok := retrieval.FAQDirectOKWithPolicy(rrfScores, vectorHits, lexicalHits, retrieval.DefaultFAQDirectPolicy())
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
	ts tenantScopedQuerier,
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
	if !s.checkTenantEmbedQuota(ctx, tenantID) {
		retrieval.RecordEmbedQuotaRejected()
		rlog.Warn("catalog embed quota exceeded, lexical fallback", "tenant", tenantID)
		return replyFromBusinessCatalog(userText, profile, catalog, history)
	}
	logCtx := ctx
	qctx, cancel := context.WithTimeout(ctx, retrievalBudget)
	defer cancel()
	tenant := retrieval.TenantIdentity{TenantID: tenantID, TenantSchema: tenantSchema}
	hits, err := svc.RetrieveCatalogCandidates(qctx, tenant, userText, 3)
	zero := len(hits) == 0
	fallback := err != nil || zero
	if fallback {
		reason := retrieval.FallbackReasonQueryError
		if err != nil {
			reason = retrieval.ClassifyVectorError(err)
		} else if zero {
			reason = retrieval.FallbackReasonQueryError
		}
		retrieval.LogQueryWithReason(logCtx, "catalog", tenantID, string(mode), err != nil, zero, reason)
		recordRetrievalQueryMetrics("catalog", string(mode), err != nil, zero, retrieval.LatencyP95Ms())
		if err != nil {
			rlog.Warn("catalog vector retrieval failed, lexical fallback",
				"err", err, "tenant", tenantID, "fallback_reason", reason)
		}
		return replyFromBusinessCatalog(userText, profile, catalog, history)
	}
	retrieval.LogQueryWithReason(logCtx, "catalog", tenantID, string(mode), false, false, "")
	recordRetrievalQueryMetrics("catalog", string(mode), false, false, retrieval.LatencyP95Ms())

	catalogExpanded := catalog
	if ts != nil {
		if missing := collectMissingCatalogIDs(catalog, hits); len(missing) > 0 {
			fetched, fetchErr := loadCatalogItemsByIDs(ctx, ts, missing)
			if fetchErr != nil {
				rlog.Warn("retrieval catalog fetch by id failed", "err", fetchErr, "tenant", tenantID, "n", len(missing))
			} else {
				catalogExpanded = mergeCatalogItems(catalog, fetched)
			}
		}
	}

	vctx := &bf.CatalogVectorContext{Hits: hits}
	return bf.ReplyFromBusinessCatalogVector(userText, profile, catalogExpanded, history, vctx)
}
