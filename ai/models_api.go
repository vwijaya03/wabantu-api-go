package ai

import (
	"context"

	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.app/wabantu/shared/types"
)

type ModelCatalogResponse struct {
	Defaults RoutingDefaults     `json:"defaults"`
	Models   []ModelCatalogEntry `json:"models"`
}

// ListModelCatalog exposes Anthropic model IDs used by WABantu (owner/staff dashboard).
//
//encore:api auth method=GET path=/api/v1/ai/models
func ListModelCatalog(ctx context.Context) (*ModelCatalogResponse, error) {
	if !hasAuthUser(ctx) {
		return nil, &errs.Error{Code: errs.Unauthenticated, Message: "not authenticated"}
	}
	return &ModelCatalogResponse{
		Defaults: DefaultRouting(),
		Models:   ModelCatalog(),
	}, nil
}

func hasAuthUser(_ context.Context) bool {
	_, ok := auth.Data().(*types.AuthUser)
	return ok
}
