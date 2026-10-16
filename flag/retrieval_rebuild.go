package flag

import (
	"context"
	"fmt"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/kb"
)

type RebuildRetrievalIndexRequest struct {
	TenantID     string   `json:"tenantId"`
	WipePinecone *bool    `json:"wipePinecone,omitempty"`
	Entities     []string `json:"entities,omitempty"` // kb, catalog — default both
}

type RebuildRetrievalIndexResponse struct {
	TenantID        string `json:"tenantId"`
	KBReset         int64  `json:"kbReset"`
	CatalogReset    int64  `json:"catalogReset"`
	KBEnqueued      int    `json:"kbEnqueued"`
	CatalogEnqueued int    `json:"catalogEnqueued"`
	PineconeWiped   bool   `json:"pineconeWiped"`
}

func rebuildIncludesFromEntities(entities []string) (includeKB, includeCatalog bool, err error) {
	if len(entities) == 0 {
		return true, true, nil
	}
	includeKB = false
	includeCatalog = false
	for _, e := range entities {
		switch e {
		case "kb":
			includeKB = true
		case "catalog":
			includeCatalog = true
		default:
			return false, false, appErrs.BadRequest(fmt.Sprintf("unknown entity %q (use kb, catalog)", e))
		}
	}
	if !includeKB && !includeCatalog {
		return false, false, appErrs.BadRequest("entities must include kb and/or catalog")
	}
	return includeKB, includeCatalog, nil
}

//encore:api auth method=POST path=/api/v1/flags/retrieval-rebuild tag:super_admin
func RebuildRetrievalIndex(ctx context.Context, req *RebuildRetrievalIndexRequest) (*RebuildRetrievalIndexResponse, error) {
	if _, err := requireSuperAdmin(); err != nil {
		return nil, err
	}
	if req == nil || req.TenantID == "" {
		return nil, appErrs.BadRequest("tenantId required")
	}

	schema, err := tenantSchemaForID(ctx, req.TenantID)
	if err != nil {
		return nil, err
	}

	includeKB, includeCatalog, err := rebuildIncludesFromEntities(req.Entities)
	if err != nil {
		return nil, err
	}

	wipePinecone := true
	if req.WipePinecone != nil {
		wipePinecone = *req.WipePinecone
	}

	result, err := kb.RebuildTenantRetrievalIndex(ctx, schema, req.TenantID, kb.RebuildTenantRetrievalOptions{
		WipePinecone:   wipePinecone,
		IncludeKB:      includeKB,
		IncludeCatalog: includeCatalog,
	})
	if err != nil {
		return nil, err
	}

	return &RebuildRetrievalIndexResponse{
		TenantID:        req.TenantID,
		KBReset:         result.KBReset,
		CatalogReset:    result.CatalogReset,
		KBEnqueued:      result.KBEnqueued,
		CatalogEnqueued: result.CatalogEnqueued,
		PineconeWiped:   result.PineconeWiped,
	}, nil
}
