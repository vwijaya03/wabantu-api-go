package inventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/tenant"
)

type BatchTrackStockParams struct {
	CatalogItemIDs []string `json:"catalogItemIds"`
	All            bool     `json:"all"`
	TrackStock     bool     `json:"trackStock"`
}

type BatchTrackStockResponse struct {
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
}

const maxBatchSkuTrack = 2000

//encore:api auth method=POST path=/api/v1/inventory/sku-batch/track
func BatchTrackStock(ctx context.Context, p *BatchTrackStockParams) (*BatchTrackStockResponse, error) {
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

	ids := uniqueNonEmpty(p.CatalogItemIDs)
	if p.All {
		rows, qerr := conn.QueryContext(ctx, `
			SELECT ci.id::text
			FROM business_catalog_item ci
			WHERE ci.deleted_at IS NULL
			ORDER BY ci.name`)
		if qerr != nil {
			return nil, appErrs.Internal(qerr.Error())
		}
		defer rows.Close()
		ids = ids[:0]
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return nil, appErrs.Internal(err.Error())
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
	}
	if len(ids) == 0 {
		return nil, appErrs.BadRequest("tidak ada produk dipilih")
	}
	if len(ids) > maxBatchSkuTrack {
		return nil, appErrs.BadRequest(fmt.Sprintf("maksimal %d produk per aksi", maxBatchSkuTrack))
	}

	resp := &BatchTrackStockResponse{}
	for _, id := range ids {
		if err := validateCatalogItem(ctx, conn, id); err != nil {
			return nil, err
		}
		isBundle, berr := catalogItemIsBundle(ctx, conn, id)
		if berr != nil {
			return nil, berr
		}
		if isBundle {
			resp.Skipped++
			continue
		}
		if p.TrackStock {
			if err := ensureSku(ctx, conn, id); err != nil {
				return nil, appErrs.Internal(err.Error())
			}
			if _, err := conn.ExecContext(ctx,
				`UPDATE inv_sku SET track_stock = true, updated_at = now() WHERE catalog_item_id = $1`, id); err != nil {
				return nil, appErrs.Internal(err.Error())
			}
		} else {
			res, err := conn.ExecContext(ctx,
				`UPDATE inv_sku SET track_stock = false, updated_at = now() WHERE catalog_item_id = $1`, id)
			if err != nil {
				return nil, appErrs.Internal(err.Error())
			}
			n, _ := res.RowsAffected()
			if n == 0 {
				resp.Skipped++
				continue
			}
		}
		resp.Updated++
	}
	return resp, nil
}

func catalogItemIsBundle(ctx context.Context, conn *sql.Conn, catalogItemID string) (bool, error) {
	var isBundle bool
	err := conn.QueryRowContext(ctx, `
		SELECT COALESCE(is_bundle, false) FROM inv_sku WHERE catalog_item_id = $1`, catalogItemID).Scan(&isBundle)
	if err == nil {
		return isBundle, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		var hasComponents bool
		err2 := conn.QueryRowContext(ctx, `
			SELECT EXISTS(SELECT 1 FROM inv_bundle_component WHERE parent_catalog_item_id = $1)`, catalogItemID).Scan(&hasComponents)
		if err2 != nil {
			return false, appErrs.Internal(err2.Error())
		}
		return hasComponents, nil
	}
	return false, appErrs.Internal(err.Error())
}
