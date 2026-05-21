package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/rlog"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
)

const EventTypeAIActivity = "ai_activity"

// AI activity purposes (what triggered the model / handler).
const (
	PurposeInboundAutoreply    = "inbound_autoreply"
	PurposeConversationSummary = "conversation_summary"
	PurposeCatalogImport       = "catalog_import"
)

// AIActivityParams is one auditable AI decision or model call for a tenant.
type AIActivityParams struct {
	TenantSchema   string
	TenantID       string
	ConversationID string
	InboundID      string
	Purpose        string
	Path           string
	Reason         string
	Model          string
	Tier           string
	LLMUsed        bool
	InputTokens    int
	OutputTokens   int
	RouteReason    string
	Classifier     string
}

// AIActivityEntry is a stored usage_event row for dashboards.
type AIActivityEntry struct {
	ID             string          `json:"id"`
	Purpose        string          `json:"purpose"`
	Path           string          `json:"path"`
	Reason         string          `json:"reason"`
	Model          string          `json:"model,omitempty"`
	Tier           string          `json:"tier,omitempty"`
	LLMUsed        bool            `json:"llmUsed"`
	InputTokens    int             `json:"inputTokens"`
	OutputTokens   int             `json:"outputTokens"`
	TotalTokens    int             `json:"totalTokens"`
	ConversationID string          `json:"conversationId,omitempty"`
	InboundID      string          `json:"inboundId,omitempty"`
	RouteReason    string          `json:"routeReason,omitempty"`
	Classifier     string          `json:"classifier,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
}

// AIActivityByPath aggregates counts per delivery path.
type AIActivityByPath struct {
	Path        string `json:"path"`
	Count       int    `json:"count"`
	LLMCalls    int    `json:"llmCalls"`
	TotalTokens int    `json:"totalTokens"`
}

// AIActivityByModel aggregates token usage per model id.
type AIActivityByModel struct {
	Model          string  `json:"model"`
	Tier           string  `json:"tier"`
	Calls          int     `json:"calls"`
	InputTokens    int     `json:"inputTokens"`
	OutputTokens   int     `json:"outputTokens"`
	EstimatedUSD   float64 `json:"estimatedCostUsd"`
}

// AIActivitySummary is a tenant-level rollup for a calendar month.
type AIActivitySummary struct {
	Period      string               `json:"period"`
	TotalEvents int                  `json:"totalEvents"`
	LLMCalls    int                  `json:"llmCalls"`
	TotalTokens int                  `json:"totalTokens"`
	ByPath      []AIActivityByPath   `json:"byPath"`
	ByModel     []AIActivityByModel  `json:"byModel"`
}

type ListAIActivityParams struct {
	Period string `query:"period"`
	Limit  int    `query:"limit"`
}

type ListAIActivityResponse struct {
	Period  string            `json:"period"`
	Entries []AIActivityEntry `json:"entries"`
}

// RecordAIActivity persists one AI activity row (usage_event) and structured log.
func RecordAIActivity(ctx context.Context, p AIActivityParams) error {
	if err := validateSchema(p.TenantSchema); err != nil {
		return err
	}
	purpose := p.Purpose
	if purpose == "" {
		purpose = PurposeInboundAutoreply
	}
	totalTok := p.InputTokens + p.OutputTokens
	if totalTok < 1 {
		totalTok = 1
	}

	meta := map[string]any{
		"purpose":        purpose,
		"path":           p.Path,
		"reason":         p.Reason,
		"model":          p.Model,
		"tier":           p.Tier,
		"llmUsed":        p.LLMUsed,
		"inputTokens":    p.InputTokens,
		"outputTokens":   p.OutputTokens,
		"conversationId": p.ConversationID,
		"inboundId":      p.InboundID,
		"routeReason":    p.RouteReason,
		"classifier":     p.Classifier,
		"tenantId":       p.TenantID,
	}
	metaJSON, _ := json.Marshal(meta)

	if err := RecordEvent(ctx, p.TenantSchema, EventTypeAIActivity, totalTok, metaJSON); err != nil {
		return err
	}

	rlog.Info("AI activity recorded",
		"tenantSchema", p.TenantSchema,
		"tenantId", p.TenantID,
		"purpose", purpose,
		"path", p.Path,
		"model", p.Model,
		"tier", p.Tier,
		"llmUsed", p.LLMUsed,
		"inputTokens", p.InputTokens,
		"outputTokens", p.OutputTokens,
		"convoId", p.ConversationID,
	)
	return nil
}

func normalizeAIActivityPeriod(period string) string {
	if period == "" {
		return time.Now().Format("2006-01")
	}
	return period
}

func normalizeAIActivityLimit(limit int) int {
	if limit < 1 || limit > 500 {
		return 100
	}
	return limit
}

// FetchAIActivityList loads AI activity rows for a tenant schema (shared with admin API).
func FetchAIActivityList(ctx context.Context, tenantSchema, period string, limit int) (*ListAIActivityResponse, error) {
	period = normalizeAIActivityPeriod(period)
	limit = normalizeAIActivityLimit(limit)
	entries, err := queryAIActivity(ctx, tenantSchema, period, limit)
	if err != nil {
		return nil, err
	}
	return &ListAIActivityResponse{Period: period, Entries: entries}, nil
}

// FetchAIActivitySummary builds monthly rollups for a tenant schema (shared with admin API).
func FetchAIActivitySummary(ctx context.Context, tenantSchema, period string) (*AIActivitySummary, error) {
	period = normalizeAIActivityPeriod(period)
	return buildAIActivitySummary(ctx, tenantSchema, period)
}

//encore:api auth method=GET path=/api/v1/usage/ai-activity
func ListAIActivity(ctx context.Context, p *ListAIActivityParams) (*ListAIActivityResponse, error) {
	u, _ := auth.Data().(*types.AuthUser)
	if u == nil || u.Role != "super_admin" || !u.Impersonating || u.TenantSchema == "" {
		return nil, appErrs.Forbidden("super_admin impersonation required")
	}
	return FetchAIActivityList(ctx, u.TenantSchema, p.Period, p.Limit)
}

//encore:api auth method=GET path=/api/v1/usage/ai-activity/summary
func GetAIActivitySummary(ctx context.Context, p *ListAIActivityParams) (*AIActivitySummary, error) {
	u, _ := auth.Data().(*types.AuthUser)
	if u == nil || u.Role != "super_admin" || !u.Impersonating || u.TenantSchema == "" {
		return nil, appErrs.Forbidden("super_admin impersonation required")
	}
	return FetchAIActivitySummary(ctx, u.TenantSchema, p.Period)
}

func queryAIActivity(ctx context.Context, tenantSchema, period string, limit int) ([]AIActivityEntry, error) {
	rows, err := db.Query(ctx, fmt.Sprintf(
		`SELECT id, quantity, metadata, created_at
		 FROM "%s".usage_event
		 WHERE event_type = $1
		   AND to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM') = $2
		 ORDER BY created_at DESC
		 LIMIT $3`, tenantSchema),
		EventTypeAIActivity, period, limit)
	if err != nil {
		return nil, appErrs.Internal("list AI activity failed")
	}
	defer rows.Close()

	out := make([]AIActivityEntry, 0)
	for rows.Next() {
		var id string
		var qty int
		var metaRaw []byte
		var createdAt time.Time
		if err := rows.Scan(&id, &qty, &metaRaw, &createdAt); err != nil {
			return nil, err
		}
		e := parseAIActivityEntry(id, qty, metaRaw, createdAt)
		out = append(out, e)
	}
	return out, rows.Err()
}

func parseAIActivityEntry(id string, qty int, metaRaw []byte, createdAt time.Time) AIActivityEntry {
	var meta map[string]any
	_ = json.Unmarshal(metaRaw, &meta)
	getStr := func(k string) string {
		v, _ := meta[k].(string)
		return v
	}
	getInt := func(k string) int {
		switch t := meta[k].(type) {
		case float64:
			return int(t)
		case int:
			return t
		default:
			return 0
		}
	}
	getBool := func(k string) bool {
		v, _ := meta[k].(bool)
		return v
	}
	in := getInt("inputTokens")
	out := getInt("outputTokens")
	return AIActivityEntry{
		ID:             id,
		Purpose:        getStr("purpose"),
		Path:           getStr("path"),
		Reason:         getStr("reason"),
		Model:          getStr("model"),
		Tier:           getStr("tier"),
		LLMUsed:        getBool("llmUsed"),
		InputTokens:    in,
		OutputTokens:   out,
		TotalTokens:    in + out,
		ConversationID: getStr("conversationId"),
		InboundID:      getStr("inboundId"),
		RouteReason:    getStr("routeReason"),
		Classifier:     getStr("classifier"),
		CreatedAt:      createdAt,
	}
}

func buildAIActivitySummary(ctx context.Context, tenantSchema, period string) (*AIActivitySummary, error) {
	entries, err := queryAIActivity(ctx, tenantSchema, period, 5000)
	if err != nil {
		return nil, err
	}
	pathMap := map[string]*AIActivityByPath{}
	modelMap := map[string]*AIActivityByModel{}
	summary := &AIActivitySummary{Period: period}

	for _, e := range entries {
		summary.TotalEvents++
		if e.LLMUsed {
			summary.LLMCalls++
		}
		summary.TotalTokens += e.TotalTokens

		pk := e.Path
		if pk == "" {
			pk = "unknown"
		}
		if pathMap[pk] == nil {
			pathMap[pk] = &AIActivityByPath{Path: pk}
		}
		pathMap[pk].Count++
		if e.LLMUsed {
			pathMap[pk].LLMCalls++
		}
		pathMap[pk].TotalTokens += e.TotalTokens

		if e.LLMUsed && e.Model != "" {
			mk := e.Model
			if modelMap[mk] == nil {
				modelMap[mk] = &AIActivityByModel{Model: mk, Tier: e.Tier}
			}
			modelMap[mk].Calls++
			modelMap[mk].InputTokens += e.InputTokens
			modelMap[mk].OutputTokens += e.OutputTokens
		}
	}

	for _, v := range pathMap {
		summary.ByPath = append(summary.ByPath, *v)
	}
	for _, v := range modelMap {
		v.EstimatedUSD = EstimateTokenCost(v.Model, v.InputTokens, v.OutputTokens)
		summary.ByModel = append(summary.ByModel, *v)
	}
	return summary, nil
}
