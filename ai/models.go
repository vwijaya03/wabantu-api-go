package ai

import (
	"strings"
)

// Official Anthropic API model IDs (pinned snapshots).
// Docs: https://docs.anthropic.com/en/docs/about-claude/models/overview
const (
	APIIDHaiku45  = "claude-haiku-4-5-20251001"
	APIIDSonnet46 = "claude-sonnet-4-6"
	APIIDOpus47   = "claude-opus-4-7"

	AliasHaiku45  = "claude-haiku-4-5"
	AliasSonnet46 = "claude-sonnet-4-6"
)

// Default models used by WABantu routing (always pinned API IDs, not legacy 3.5).
const (
	ModelHaiku  = APIIDHaiku45
	ModelSonnet = APIIDSonnet46
)

// ModelStatus describes whether WABantu will call this ID.
type ModelStatus string

const (
	ModelStatusActive  ModelStatus = "active"
	ModelStatusLegacy  ModelStatus = "legacy"  // mapped or fallback only
	ModelStatusRetired ModelStatus = "retired" // do not call; 404 on API
)

// ModelUse describes where WABantu uses the model.
type ModelUse string

const (
	ModelUseAutoReply  ModelUse = "auto_reply"
	ModelUseSummarize  ModelUse = "summarize"
	ModelUseCostEstimate ModelUse = "cost_estimate"
)

// ModelCatalogEntry is one row in the model registry (for API + ops).
type ModelCatalogEntry struct {
	APIID       string      `json:"apiId"`
	Alias       string      `json:"alias,omitempty"`
	DisplayName string      `json:"displayName"`
	Tier        string      `json:"tier"` // haiku | sonnet | opus
	Status      ModelStatus `json:"status"`
	Uses        []ModelUse  `json:"uses"`
	Notes       string      `json:"notes,omitempty"`
}

// modelRegistry is the source of truth for Anthropic models in WABantu.
var modelRegistry = []ModelCatalogEntry{
	{
		APIID: APIIDHaiku45, Alias: AliasHaiku45,
		DisplayName: "Claude Haiku 4.5",
		Tier: "haiku", Status: ModelStatusActive,
		Uses: []ModelUse{ModelUseAutoReply, ModelUseSummarize, ModelUseCostEstimate},
		Notes: "Default for starter / simple hybrid / conversation summarize",
	},
	{
		APIID: APIIDSonnet46, Alias: AliasSonnet46,
		DisplayName: "Claude Sonnet 4.6",
		Tier: "sonnet", Status: ModelStatusActive,
		Uses: []ModelUse{ModelUseAutoReply},
		Notes: "Default for complex hybrid / business & pro plans",
	},
	{
		APIID: APIIDOpus47, Alias: "claude-opus-4-7",
		DisplayName: "Claude Opus 4.7",
		Tier: "opus", Status: ModelStatusLegacy,
		Uses: nil,
		Notes: "Available on Anthropic API; not used by WABantu routing yet",
	},
	{
		APIID: "claude-sonnet-4-5-20250929", Alias: "claude-sonnet-4-5",
		DisplayName: "Claude Sonnet 4.5",
		Tier: "sonnet", Status: ModelStatusLegacy,
		Uses: nil,
		Notes: "Fallback if Sonnet 4.6 unavailable on account",
	},
	{
		APIID: "claude-3-5-haiku-20241022",
		DisplayName: "Claude 3.5 Haiku (retired)",
		Tier: "haiku", Status: ModelStatusRetired,
		Uses: nil,
		Notes: "Returns 404 — replaced by claude-haiku-4-5-20251001",
	},
	{
		APIID: "claude-sonnet-4-5-20250514",
		DisplayName: "Claude Sonnet 4.5 (invalid snapshot)",
		Tier: "sonnet", Status: ModelStatusRetired,
		Uses: nil,
		Notes: "Returns 404 — use claude-sonnet-4-6 or claude-sonnet-4-5-20250929",
	},
	{
		APIID: "claude-3-5-sonnet-20241022",
		DisplayName: "Claude 3.5 Sonnet",
		Tier: "sonnet", Status: ModelStatusRetired,
		Uses: nil,
		Notes: "Legacy; not used",
	},
}

// aliasToAPIID maps convenience aliases and old IDs to active pinned IDs.
var aliasToAPIID = map[string]string{
	AliasHaiku45:              APIIDHaiku45,
	"claude-3-5-haiku-20241022": APIIDHaiku45,
	AliasSonnet46:             APIIDSonnet46,
	"claude-sonnet-4-5":       "claude-sonnet-4-5-20250929",
	"claude-sonnet-4-5-20250514": APIIDSonnet46,
}

// fallbackByTier is tried in order when the API returns model not_found.
var fallbackByTier = map[string][]string{
	"haiku":  {APIIDHaiku45, AliasHaiku45},
	"sonnet": {APIIDSonnet46, "claude-sonnet-4-5-20250929", AliasSonnet46},
}

// DefaultHaikuAPIID returns the pinned Haiku model for API calls (canonical alias).
func DefaultHaikuAPIID() string { return AliasHaiku45 }

// DefaultSonnetAPIID returns the pinned Sonnet model for API calls.
func DefaultSonnetAPIID() string { return APIIDSonnet46 }

// ModelCatalog returns a copy of the registry for dashboards and debugging.
func ModelCatalog() []ModelCatalogEntry {
	out := make([]ModelCatalogEntry, len(modelRegistry))
	copy(out, modelRegistry)
	return out
}

// ActiveModelsForUse returns active API IDs used for a given purpose.
func ActiveModelsForUse(use ModelUse) []string {
	var ids []string
	for _, e := range modelRegistry {
		if e.Status != ModelStatusActive {
			continue
		}
		for _, u := range e.Uses {
			if u == use {
				ids = append(ids, e.APIID)
				break
			}
		}
	}
	return ids
}

// ResolveAnthropicModel normalizes aliases and legacy IDs to a concrete API model id.
func ResolveAnthropicModel(requested string) string {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return DefaultSonnetAPIID()
	}
	if mapped, ok := aliasToAPIID[requested]; ok {
		return mapped
	}
	return requested
}

// TierForModel returns haiku | sonnet | opus | unknown for fallback selection.
func TierForModel(model string) string {
	m := strings.ToLower(ResolveAnthropicModel(model))
	switch {
	case strings.Contains(m, "haiku"):
		return "haiku"
	case strings.Contains(m, "sonnet"):
		return "sonnet"
	case strings.Contains(m, "opus"):
		return "opus"
	default:
		return "unknown"
	}
}

// FallbackModels returns models to try after a not_found error (includes requested first).
func FallbackModels(requested string) []string {
	resolved := ResolveAnthropicModel(requested)
	tier := TierForModel(resolved)
	chain, ok := fallbackByTier[tier]
	if !ok {
		return []string{resolved, DefaultHaikuAPIID()}
	}
	seen := make(map[string]struct{})
	var out []string
	add := func(id string) {
		id = ResolveAnthropicModel(id)
		if id == "" {
			return
		}
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	add(resolved)
	for _, id := range chain {
		add(id)
	}
	return out
}

// RoutingDefaults documents which models plans use (for catalog API).
type RoutingDefaults struct {
	StarterHaiku     string `json:"starterHaiku"`
	HybridSimple     string `json:"hybridSimple"`
	HybridComplex    string `json:"hybridComplex"`
	Summarize        string `json:"summarize"`
}

// DefaultRouting returns pinned IDs used by ResolveRouting + summarize.
func DefaultRouting() RoutingDefaults {
	return RoutingDefaults{
		StarterHaiku:  DefaultHaikuAPIID(),
		HybridSimple:  DefaultHaikuAPIID(),
		HybridComplex: DefaultSonnetAPIID(),
		Summarize:     DefaultHaikuAPIID(),
	}
}
