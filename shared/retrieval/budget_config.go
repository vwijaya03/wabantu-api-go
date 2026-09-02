package retrieval

import (
	"strconv"
	"strings"
	"time"

	"encore.dev"
)

const (
	budgetMinMs = 200
	budgetMaxMs = 10_000

	budgetDevelopment = 2500 * time.Millisecond
	budgetStaging     = 1200 * time.Millisecond
	budgetProduction  = 1200 * time.Millisecond
)

// QueryBudget returns the nominal retrieval sub-budget for the current environment.
// Override at runtime via Encore secret RetrievalBudgetMs (clamped 200ms–10s).
func QueryBudget() time.Duration {
	if override := parseBudgetOverride(secrets.RetrievalBudgetMs); override > 0 {
		return override
	}
	meta := encore.Meta()
	switch strings.ToLower(string(meta.Environment.Type)) {
	case "production", "prod":
		return budgetProduction
	case "staging":
		return budgetStaging
	default:
		if meta.Environment.Cloud == encore.CloudLocal {
			return budgetDevelopment
		}
		return budgetStaging
	}
}

func parseBudgetOverride(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms < budgetMinMs {
		if ms > 0 && ms < budgetMinMs {
			ms = budgetMinMs
		} else {
			return 0
		}
	}
	if ms > budgetMaxMs {
		ms = budgetMaxMs
	}
	return time.Duration(ms) * time.Millisecond
}
