package business

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	apperr "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/triagerepair"
)

type TriageCatalogPatchParams struct {
	TenantSchema string `json:"tenantSchema"`
	ItemID       string `json:"itemId"`
	Field        string `json:"field"`
	Value        string `json:"value"`
	Apply        bool   `json:"apply"`
}

type TriageCatalogPatchResult struct {
	BeforeHash string `json:"beforeHash"`
	AfterHash  string `json:"afterHash"`
	Before     string `json:"before"`
	After      string `json:"after"`
}

// ApplyTriageCatalogPatch updates one allowlisted catalog field (private).
//
//encore:api private method=POST path=/api/v1/internal/business/triage-catalog-patch
func ApplyTriageCatalogPatch(ctx context.Context, p *TriageCatalogPatchParams) (*TriageCatalogPatchResult, error) {
	if p == nil || strings.TrimSpace(p.TenantSchema) == "" || strings.TrimSpace(p.ItemID) == "" {
		return nil, apperr.BadRequest("tenantSchema and itemId required")
	}
	if !triagerepair.AllowedCatalogFields[p.Field] {
		return nil, apperr.BadRequest("field katalog tidak diizinkan")
	}
	ts, err := openTenantScope(ctx, p.TenantSchema)
	if err != nil {
		return nil, err
	}
	var name string
	var price float64
	var active bool
	err = ts.QueryRowContext(ctx, `
		SELECT name, sell_price, is_active FROM business_catalog_item
		WHERE id = $1::uuid AND deleted_at IS NULL`, p.ItemID).Scan(&name, &price, &active)
	if err == sql.ErrNoRows {
		return nil, apperr.NotFound("katalog tidak ditemukan")
	}
	if err != nil {
		return nil, err
	}
	before := fmt.Sprintf("%s|%v|%v", name, price, active)
	afterName, afterPrice, afterActive := name, price, active
	switch p.Field {
	case "name":
		afterName = strings.TrimSpace(p.Value)
	case "sell_price":
		afterPrice, _ = strconv.ParseFloat(p.Value, 64)
	case "is_active":
		afterActive = strings.EqualFold(p.Value, "true") || p.Value == "1"
	}
	after := fmt.Sprintf("%s|%v|%v", afterName, afterPrice, afterActive)
	res := &TriageCatalogPatchResult{
		Before:     before,
		After:      after,
		BeforeHash: hashText(before),
		AfterHash:  hashText(after),
	}
	if !p.Apply {
		return res, nil
	}
	_, err = ts.ExecContext(ctx, `
		UPDATE business_catalog_item
		SET name = $2, sell_price = $3, is_active = $4, updated_at = now()
		WHERE id = $1::uuid AND deleted_at IS NULL`,
		p.ItemID, afterName, afterPrice, afterActive)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
