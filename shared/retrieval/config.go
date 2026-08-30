package retrieval

// Secrets holds RAG credentials (Encore secrets).
var secrets struct {
	OpenAIApiKey      string
	PineconeApiKey    string
	PineconeIndexHost string
}

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

// DefaultService returns production service or nil (callers fall back to lexical).
func DefaultService() *Service {
	return NewProductionService()
}
