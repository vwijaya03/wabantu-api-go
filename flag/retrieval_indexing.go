package flag

import (
	"context"

	"encore.app/wabantu/kb"
	"encore.app/wabantu/shared/retrieval"
)

//encore:api auth method=GET path=/api/v1/flags/retrieval-indexing/:tenantId
func GetRetrievalIndexingProgress(ctx context.Context, tenantId string) (*kb.TenantIndexingProgress, error) {
	if _, err := requireSuperAdmin(); err != nil {
		return nil, err
	}
	schema, err := tenantSchemaForID(ctx, tenantId)
	if err != nil {
		return nil, err
	}
	return kb.GetTenantIndexingProgress(ctx, schema, tenantId)
}

type RetrievalObservabilityResponse struct {
	Metrics retrieval.ObservabilitySnapshot `json:"metrics"`
}

//encore:api auth method=GET path=/api/v1/flags/retrieval-observability
func GetRetrievalObservability(ctx context.Context) (*RetrievalObservabilityResponse, error) {
	if _, err := requireSuperAdmin(); err != nil {
		return nil, err
	}
	return &RetrievalObservabilityResponse{
		Metrics: retrieval.SnapshotObservabilityWithBreakers(retrieval.DefaultServiceBreakers()),
	}, nil
}
