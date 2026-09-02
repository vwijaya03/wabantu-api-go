package ai

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"encore.app/wabantu/shared/retrieval"
)

const (
	retrievalIncidentsKey = "ai:retrieval:incidents"
	retrievalIncidentsMax = 200
	retrievalIncidentsTTL = 7 * 24 * time.Hour
)

var (
	urlPattern    = regexp.MustCompile(`https?://\S+`)
	queryParamPat = regexp.MustCompile(`[?&][a-zA-Z0-9_]+=[^\s&]+`)
)

// RetrievalIncident is a sanitized retrieval failure stored for superadmin triage.
type RetrievalIncident struct {
	At        time.Time              `json:"at"`
	TenantID  string                 `json:"tenantId"`
	Source    string                 `json:"source"`
	Provider  retrieval.Provider     `json:"provider"`
	Category  retrieval.ErrorCategory `json:"category"`
	LatencyMs int                    `json:"latencyMs"`
	BudgetMs  int                    `json:"budgetMs"`
	SafeError string                 `json:"safeError"`
}

type retrievalIncidentInput struct {
	TenantID  string
	Source    string
	Provider  retrieval.Provider
	Category  retrieval.ErrorCategory
	LatencyMs int
	BudgetMs  int
	Err       error
}

// SanitizeRetrievalError strips URLs, query params, and truncates error text for storage.
func SanitizeRetrievalError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	msg = urlPattern.ReplaceAllString(msg, "[url]")
	msg = queryParamPat.ReplaceAllString(msg, "")
	msg = strings.TrimSpace(msg)
	if len(msg) > 256 {
		msg = msg[:256]
	}
	return msg
}

func recordRetrievalIncident(ctx context.Context, in retrievalIncidentInput) {
	if in.Err == nil && in.Category == "" {
		return
	}
	inc := RetrievalIncident{
		At:        time.Now().UTC(),
		TenantID:  in.TenantID,
		Source:    in.Source,
		Provider:  in.Provider,
		Category:  in.Category,
		LatencyMs: in.LatencyMs,
		BudgetMs:  in.BudgetMs,
		SafeError: SanitizeRetrievalError(in.Err),
	}
	if svc == nil || svc.rdb == nil {
		return
	}
	raw, err := json.Marshal(inc)
	if err != nil {
		return
	}
	pipe := svc.rdb.Pipeline()
	pipe.LPush(ctx, retrievalIncidentsKey, raw)
	pipe.LTrim(ctx, retrievalIncidentsKey, 0, retrievalIncidentsMax-1)
	pipe.Expire(ctx, retrievalIncidentsKey, retrievalIncidentsTTL)
	_, _ = pipe.Exec(ctx)
}

// RecentRetrievalIncidents returns the most recent sanitized incidents (newest first).
func RecentRetrievalIncidents(ctx context.Context, limit int) ([]RetrievalIncident, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > retrievalIncidentsMax {
		limit = retrievalIncidentsMax
	}
	if svc == nil || svc.rdb == nil {
		return nil, errors.New("redis unavailable")
	}
	raw, err := svc.rdb.LRange(ctx, retrievalIncidentsKey, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}
	out := make([]RetrievalIncident, 0, len(raw))
	for _, item := range raw {
		var inc RetrievalIncident
		if json.Unmarshal([]byte(item), &inc) == nil {
			out = append(out, inc)
		}
	}
	return out, nil
}
