package kbcontext

import (
	"context"

	"encore.app/wabantu/shared/retrieval"
)

// CatalogResult holds catalog retrieval trace for grounding.
type CatalogResult struct {
	Trace      Trace
	Hits       []retrieval.Hit
	Degraded   DegradedMode
	UsedVector bool
}

// RetrieveCatalog runs vector catalog search when mode allows.
func RetrieveCatalog(ctx context.Context, tenant TenantRef, query string, mode retrieval.RetrievalMode, topK int) (CatalogResult, error) {
	if topK <= 0 {
		topK = 3
	}
	out := CatalogResult{Trace: Trace{Version: CaptureVersion, Query: query, Mode: string(mode)}}

	svc := retrieval.DefaultService()
	if svc == nil || mode != retrieval.ModeVector {
		out.Trace.Hash = HashTrace(out.Trace)
		return out, nil
	}

	tenantID := retrieval.TenantIdentity{TenantID: tenant.TenantID, TenantSchema: tenant.TenantSchema}
	hits, err := svc.RetrieveCatalogCandidates(ctx, ctx, tenantID, query, topK)
	if err != nil {
		out.Degraded = DegradedEmbedQuota
		out.Trace.LexicalFallback = true
		out.Trace.FallbackReason = err.Error()
		out.Trace.Hash = HashTrace(out.Trace)
		return out, err
	}
	out.Hits = hits
	out.UsedVector = true
	out.Trace.UsedVector = true
	out.Trace.CatalogIDs = catalogIDsFromHits(hits)
	out.Trace.Hash = HashTrace(out.Trace)
	return out, nil
}

func catalogIDsFromHits(hits []retrieval.Hit) []string {
	out := make([]string, 0, len(hits))
	seen := map[string]struct{}{}
	for _, h := range hits {
		id, _ := h.Metadata["entry_id"].(string)
		if id == "" {
			id = h.ID
		}
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
