package retrieval

import (
	"fmt"
	"time"
)

// SmokeRegressionResult mirrors key shared/retrieval unit checks.
type SmokeRegressionResult struct {
	Passed     bool   `json:"passed"`
	DurationMs int64  `json:"durationMs"`
	Error      string `json:"error,omitempty"`
}

// RunSmokeRegression runs in-memory retrieval regression checks (no Pinecone/Postgres).
func RunSmokeRegression() SmokeRegressionResult {
	start := time.Now()
	if err := smokeFilterHitsByScore(); err != nil {
		return SmokeRegressionResult{Passed: false, Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	if err := smokeFilterScoredEntries(); err != nil {
		return SmokeRegressionResult{Passed: false, Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	if err := smokeFAQDirectOKWithPolicy(); err != nil {
		return SmokeRegressionResult{Passed: false, Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	if err := smokeFAQDirectLowVector(); err != nil {
		return SmokeRegressionResult{Passed: false, Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	return SmokeRegressionResult{Passed: true, DurationMs: time.Since(start).Milliseconds()}
}

func smokeFilterHitsByScore() error {
	hits := []Hit{
		{ID: "a", Score: 0.45},
		{ID: "b", Score: 0.25},
		{ID: "c", Score: 0.05},
	}
	out := FilterHitsByScore(hits, VectorMinSimilarity)
	if len(out) != 1 || out[0].ID != "a" {
		return fmt.Errorf("FilterHitsByScore: expected only a, got %+v", out)
	}
	return nil
}

func smokeFilterScoredEntries() error {
	entries := []ScoredEntry{
		{EntryID: "x", Score: 0.12},
		{EntryID: "y", Score: 0.05},
	}
	out := FilterScoredEntries(entries, LexicalMinScore)
	if len(out) != 1 || out[0].EntryID != "x" {
		return fmt.Errorf("FilterScoredEntries: expected x only, got %+v", out)
	}
	return nil
}

func smokeFAQDirectOKWithPolicy() error {
	scores := []ScoredEntry{
		{EntryID: "faq-1", Score: 0.033},
		{EntryID: "faq-2", Score: 0.016},
	}
	vector := []Hit{{ID: "kb:faq-1:v1:c0", Score: 0.55, Metadata: map[string]any{"entry_id": "faq-1"}}}
	lexical := []ScoredEntry{{EntryID: "faq-1", Score: 0.15}}
	top, ok := FAQDirectOKWithPolicy(scores, vector, lexical, DefaultFAQDirectPolicy())
	if !ok || top.EntryID != "faq-1" {
		return fmt.Errorf("FAQDirectOKWithPolicy: expected faq-1 direct, got ok=%v top=%+v", ok, top)
	}
	scores2 := []ScoredEntry{{EntryID: "faq-9", Score: 0.017}}
	vector2 := []Hit{{ID: "kb:faq-9:v1:c0", Score: 0.6, Metadata: map[string]any{"entry_id": "faq-9"}}}
	_, ok = FAQDirectOKWithPolicy(scores2, vector2, nil, DefaultFAQDirectPolicy())
	if ok {
		return fmt.Errorf("FAQDirectOKWithPolicy: expected reject without lexical presence")
	}
	return nil
}

func smokeFAQDirectLowVector() error {
	scores := []ScoredEntry{{EntryID: "faq-1", Score: 0.033}}
	vector := []Hit{{ID: "kb:faq-1:v1:c0", Score: 0.12, Metadata: map[string]any{"entry_id": "faq-1"}}}
	lexical := []ScoredEntry{{EntryID: "faq-1", Score: 0.2}}
	_, ok := FAQDirectOKWithPolicy(scores, vector, lexical, DefaultFAQDirectPolicy())
	if ok {
		return fmt.Errorf("FAQDirectOKWithPolicy: expected reject on low vector similarity")
	}
	return nil
}
