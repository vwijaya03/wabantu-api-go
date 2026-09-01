package retrieval

// Calibrated via shared/retrieval/eval (RRF scale, not lexical 0.72).
const (
	DefaultFAQMinScore  = 0.014
	DefaultFAQMinMargin = 0.003
)

// FAQDirectOK checks top1/top2 margin guards for RRF scores.
func FAQDirectOK(scores []ScoredEntry, minScore, minMargin float64) (top ScoredEntry, ok bool) {
	if minScore <= 0 {
		minScore = DefaultFAQMinScore
	}
	if minMargin <= 0 {
		minMargin = DefaultFAQMinMargin
	}
	if len(scores) == 0 {
		return ScoredEntry{}, false
	}
	top = scores[0]
	if top.Score < minScore {
		return ScoredEntry{}, false
	}
	if len(scores) > 1 {
		margin := top.Score - scores[1].Score
		if margin < minMargin {
			return ScoredEntry{}, false
		}
	}
	return top, true
}
