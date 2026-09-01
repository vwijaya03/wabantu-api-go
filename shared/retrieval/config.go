package retrieval

import "sync"

// Secrets holds RAG credentials (Encore secrets).
var secrets struct {
	OpenAIApiKey      string
	PineconeApiKey    string
	PineconeIndexHost string
}

var (
	defaultService     *Service
	defaultServiceOnce sync.Once
)

// NewProductionService builds a Service from Encore secrets when configured.
func NewProductionService() *Service {
	openKey := secrets.OpenAIApiKey
	pineKey := secrets.PineconeApiKey
	pineHost := secrets.PineconeIndexHost
	if !OpenAIConfigured(openKey) || !PineconeConfigured(pineHost, pineKey) {
		return nil
	}
	emb := NewCachingEmbedder(NewOpenAIEmbedder(openKey))
	store := NewPineconeClient(pineHost, pineKey)
	return NewService(emb, store)
}

// DefaultService returns the singleton production service or nil when not configured.
func DefaultService() *Service {
	defaultServiceOnce.Do(func() {
		defaultService = NewProductionService()
	})
	return defaultService
}
