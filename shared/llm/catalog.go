package llm

import (
	"sort"
	"sync"
)

// Model status values exposed in the admin catalog.
const (
	ModelSupported     = "supported"      // usable for chat/intent/vision/ocr functions
	ModelEmbeddingOnly = "embedding_only" // OpenAI/Gemini embedding models: no chat adapter
)

type ModelInfo struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	Label    string `json:"label"`
	Status   string `json:"status"`
	Vision   bool   `json:"vision"`
}

var modelMeta = map[string]struct {
	label  string
	vision bool
}{
	"claude-haiku-4-5":           {"Claude Haiku 4.5", true},
	"claude-haiku-4-5-20251001":  {"Claude Haiku 4.5 (2025-10-01)", true},
	"claude-sonnet-4-5":          {"Claude Sonnet 4.5", true},
	"claude-sonnet-4-5-20250929": {"Claude Sonnet 4.5 (2025-09-29)", true},
	"claude-sonnet-4-6":          {"Claude Sonnet 4.6", true},
	"gemini-3.8-flash":           {"Gemini 3.8 Flash", true},
	"gemini-3.6-flash":           {"Gemini 3.6 Flash", true},
	"gemini-3.5-flash-lite":      {"Gemini 3.5 Flash-Lite", true},
	"gemini-3.1-pro-preview":     {"Gemini 3.1 Pro (Preview)", true},
	"gemini-3.1-flash-lite":      {"Gemini 3.1 Flash-Lite", true},
	"gemini-2.5-flash-lite":      {"Gemini 2.5 Flash-Lite", true},
	"gemini-2.5-flash":           {"Gemini 2.5 Flash", true},
	"gemini-2.5-pro":             {"Gemini 2.5 Pro", true},
	"text-embedding-3-small":     {"OpenAI text-embedding-3-small", false},
	"gemini-embedding-001":       {"Gemini Embedding 001 (1536 dim)", false},
}

// CatalogModels lists allowlisted models in stable order.
func CatalogModels() []ModelInfo {
	var out []ModelInfo
	for p, ids := range ModelAllowlist {
		for _, id := range ids {
			m := modelMeta[id]
			st := ModelSupported
			if IsEmbeddingModel(id) {
				st = ModelEmbeddingOnly
			}
			label := m.label
			if label == "" {
				label = id
			}
			out = append(out, ModelInfo{Provider: p, ID: id, Label: label, Status: st, Vision: m.vision})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// KnownProviders in stable order.
func KnownProviders() []string {
	out := make([]string, 0, len(ModelAllowlist))
	for p := range ModelAllowlist {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Process-wide runtime state set by the ai service at startup (same binary as admin).
var (
	availMu      sync.RWMutex
	availability = map[string]bool{}
	invalidate   []func()
)

// SetProviderAvailable records whether a provider has credentials and is registered.
func SetProviderAvailable(id string, ok bool) {
	availMu.Lock()
	availability[id] = ok
	availMu.Unlock()
}

// ProviderAvailable reports runtime availability (false if unknown).
func ProviderAvailable(id string) bool {
	availMu.RLock()
	defer availMu.RUnlock()
	return availability[id]
}

// OnConfigChanged registers a cache invalidation hook (e.g. Resolver.Invalidate).
func OnConfigChanged(fn func()) {
	availMu.Lock()
	invalidate = append(invalidate, fn)
	availMu.Unlock()
}

// NotifyConfigChanged runs invalidation hooks in this process. Other instances
// pick up changes within the resolver TTL (≤30s).
func NotifyConfigChanged() {
	availMu.RLock()
	hs := append([]func(){}, invalidate...)
	availMu.RUnlock()
	for _, h := range hs {
		h()
	}
}
