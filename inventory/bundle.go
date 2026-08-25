package inventory

import (
	"context"
	"database/sql"
	"math"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/tenant"
)

// BundleComponent is one child SKU of a bundle and the qty consumed per bundle unit.
type BundleComponent struct {
	ChildCatalogItemID string  `json:"childCatalogItemId"`
	Qty                float64 `json:"qty"`
}

// ComponentIssue is the resolved child quantity to move when a bundle is issued.
type ComponentIssue struct {
	CatalogItemID string
	Qty           float64
}

// ---------- pure helpers (unit-tested) ----------

// explodeBundle expands a bundle quantity into child SKU issues.
func explodeBundle(components []BundleComponent, bundleQty float64) []ComponentIssue {
	out := make([]ComponentIssue, 0, len(components))
	for _, c := range components {
		out = append(out, ComponentIssue{
			CatalogItemID: c.ChildCatalogItemID,
			Qty:           round4(c.Qty * bundleQty),
		})
	}
	return out
}

// bundleAvailableQty returns how many whole bundles can be fulfilled given each
// child's on-hand and required qty per bundle. Returns 0 if any component is out.
func bundleAvailableQty(onHandByChild map[string]float64, components []BundleComponent) float64 {
	if len(components) == 0 {
		return 0
	}
	min := math.MaxFloat64
	for _, c := range components {
		if c.Qty <= epsilon {
			continue
		}
		possible := math.Floor((onHandByChild[c.ChildCatalogItemID] + epsilon) / c.Qty)
		if possible < min {
			min = possible
		}
	}
	if min == math.MaxFloat64 || min < 0 {
		return 0
	}
	return min
}

// validateBundleComponents checks structural validity (pure).
func validateBundleComponents(parentID string, comps []BundleComponent) error {
	if len(comps) == 0 {
		return appErrs.BadRequest("bundle harus punya minimal 1 komponen")
	}
	seen := map[string]bool{}
	for _, c := range comps {
		id := strings.TrimSpace(c.ChildCatalogItemID)
		if id == "" {
			return appErrs.BadRequest("komponen bundle tidak boleh kosong")
		}
		if id == strings.TrimSpace(parentID) {
			return appErrs.BadRequest("bundle tidak boleh memuat dirinya sendiri")
		}
		if c.Qty <= epsilon {
			return appErrs.BadRequest("qty komponen harus lebih dari 0")
		}
		if seen[id] {
			return appErrs.BadRequest("komponen duplikat dalam bundle")
		}
		seen[id] = true
	}
	return nil
}

// ---------- DB helpers (reused by order flow in A8) ----------

// rowsQuerier is satisfied by *sql.Conn and *sql.Tx (adds QueryContext to querier).
type rowsQuerier interface {
	querier
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func loadBundleComponents(ctx context.Context, q rowsQuerier, parentID string) ([]BundleComponent, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT child_catalog_item_id, qty FROM inv_bundle_component WHERE parent_catalog_item_id = $1`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]BundleComponent, 0)
	for rows.Next() {
		var c BundleComponent
		if err := rows.Scan(&c.ChildCatalogItemID, &c.Qty); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func isBundleItem(ctx context.Context, q querier, catalogItemID string) (bool, error) {
	var isBundle sql.NullBool
	err := q.QueryRowContext(ctx,
		`SELECT is_bundle FROM inv_sku WHERE catalog_item_id = $1`, catalogItemID).Scan(&isBundle)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return isBundle.Valid && isBundle.Bool, nil
}

// ---------- endpoints ----------

type BundleComponentRow struct {
	ChildCatalogItemID string  `json:"childCatalogItemId"`
	ChildName          string  `json:"childName"`
	ChildExternalCode  string  `json:"childExternalCode"`
	Qty                float64 `json:"qty"`
}

type GetBundleResponse struct {
	CatalogItemID string               `json:"catalogItemId"`
	IsBundle      bool                 `json:"isBundle"`
	Components    []BundleComponentRow `json:"components"`
}

//encore:api auth method=GET path=/api/v1/inventory/bundles/:catalogItemID/components
func GetBundleComponents(ctx context.Context, catalogItemID string) (*GetBundleResponse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)

	isBundle, err := isBundleItem(ctx, conn, catalogItemID)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	rows, err := conn.QueryContext(ctx, `
		SELECT bc.child_catalog_item_id, COALESCE(ci.name, ''), COALESCE(ci.external_code, ''), bc.qty
		FROM inv_bundle_component bc
		LEFT JOIN business_catalog_item ci ON ci.id = bc.child_catalog_item_id
		WHERE bc.parent_catalog_item_id = $1
		ORDER BY ci.name`, catalogItemID)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	comps := make([]BundleComponentRow, 0)
	for rows.Next() {
		var c BundleComponentRow
		if err := rows.Scan(&c.ChildCatalogItemID, &c.ChildName, &c.ChildExternalCode, &c.Qty); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		comps = append(comps, c)
	}
	return &GetBundleResponse{CatalogItemID: catalogItemID, IsBundle: isBundle, Components: comps}, nil
}

type SetBundleParams struct {
	Components []BundleComponent `json:"components"`
}

//encore:api auth method=PUT path=/api/v1/inventory/bundles/:catalogItemID/components
func SetBundleComponents(ctx context.Context, catalogItemID string, p *SetBundleParams) (*GetBundleResponse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}

	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)

	if err := validateCatalogItem(ctx, conn, catalogItemID); err != nil {
		return nil, err
	}

	clearing := len(p.Components) == 0
	if !clearing {
		if err := validateBundleComponents(catalogItemID, p.Components); err != nil {
			return nil, err
		}
		// Children must exist and must not themselves be bundles (no nesting in v1).
		for _, c := range p.Components {
			if err := validateCatalogItem(ctx, conn, c.ChildCatalogItemID); err != nil {
				return nil, err
			}
			childIsBundle, err := isBundleItem(ctx, conn, c.ChildCatalogItemID)
			if err != nil {
				return nil, appErrs.Internal(err.Error())
			}
			if childIsBundle {
				return nil, appErrs.BadRequest("komponen tidak boleh berupa bundle (bundle bertingkat belum didukung)")
			}
		}
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tx.Rollback()

	if err := ensureSku(ctx, tx, catalogItemID); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM inv_bundle_component WHERE parent_catalog_item_id = $1`, catalogItemID); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	for _, c := range p.Components {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO inv_bundle_component (parent_catalog_item_id, child_catalog_item_id, qty)
			VALUES ($1, $2, $3)`, catalogItemID, c.ChildCatalogItemID, round4(c.Qty)); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
	}
	// A bundle parent does not hold its own stock; flag it and disable stock tracking.
	if _, err := tx.ExecContext(ctx, `
		UPDATE inv_sku SET is_bundle = $2, track_stock = $3, updated_at = now()
		WHERE catalog_item_id = $1`, catalogItemID, !clearing, clearing); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := tx.Commit(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return GetBundleComponents(ctx, catalogItemID)
}
