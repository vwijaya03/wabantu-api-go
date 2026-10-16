package retrieval

import "sync"

// Secrets holds RAG credentials (Encore secrets).
var secrets struct {
	OpenAIApiKey      string
	PineconeApiKey    string
	PineconeIndexHost string
	RetrievalBudgetMs string
}

var (
	defaultService     *Service
	defaultServiceOnce sync.Once
)

// NewProductionService builds a Service from Encore secrets when configured.
// Embedders are registered per model; the active one is resolved per tenant from
// ai_function_config (function "embedding"). OpenAI stays the default.
func NewProductionService() *Service {
	openKey := secrets.OpenAIApiKey
	pineKey := secrets.PineconeApiKey
	pineHost := secrets.PineconeIndexHost
	if !PineconeConfigured(pineHost, pineKey) {
		return nil
	}
	embedders := map[string]Embedder{}
	if OpenAIConfigured(openKey) {
		openEmb := NewOpenAIEmbedder(openKey)
		WarmupOpenAIConnection(openKey, openEmb.HTTPClient())
		embedders[EmbeddingModel] = NewCachingEmbedder(openEmb)
	}
	if key := GeminiAPIKeyFromEnv(); key != "" {
		embedders[GeminiEmbeddingModel] = NewCachingEmbedder(NewGeminiEmbedder(key))
	}
	if len(embedders) == 0 {
		return nil
	}
	store := NewPineconeClient(pineHost, pineKey)
	svc := NewService(embedders[EmbeddingModel], store)
	svc.Embedders = embedders
	return svc
}

// DefaultService returns the singleton production service or nil when not configured.
func DefaultService() *Service {
	defaultServiceOnce.Do(func() {
		defaultService = NewProductionService()
	})
	return defaultService
}

// DefaultServiceBreakers returns the breaker pool from the default service, if any.
func DefaultServiceBreakers() *BreakerPool {
	svc := DefaultService()
	if svc == nil {
		return nil
	}
	return svc.Breakers
}
