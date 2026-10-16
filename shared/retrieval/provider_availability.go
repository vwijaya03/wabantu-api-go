package retrieval

import "encore.app/wabantu/shared/llm"

// SyncOpenAIProviderAvailability updates the admin catalog "available" flag for OpenAI
// from the configured embedding API key (Encore secret / Cloud Run env injection).
func SyncOpenAIProviderAvailability() {
	llm.SetProviderAvailable("openai", OpenAIConfigured(secrets.OpenAIApiKey))
}
