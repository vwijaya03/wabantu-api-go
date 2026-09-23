package templates

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"encore.dev/beta/auth"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/entitlement"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/usage"
)

const platformFeePercent = 20

type PurchaseTemplateResponse struct {
	PurchaseID    string    `json:"purchaseId"`
	AmountIDR     int       `json:"amountIdr"`
	QRURL         string    `json:"qrUrl"`
	ExpiresAt     time.Time `json:"expiresAt"`
	MidtransOrder string    `json:"midtransOrderId"`
}

//encore:api auth method=POST path=/api/v1/tenant/templates/:slug/purchase tag:owner
func PurchaseTemplate(ctx context.Context, slug string) (*PurchaseTemplateResponse, error) {
	u, ok := auth.Data().(*types.AuthUser)
	if !ok || u == nil {
		return nil, appErrs.Unauthenticated("not authenticated")
	}
	plan := usage.TenantPlan(ctx, u.TenantSchema)
	if !entitlement.HasFeature(plan, entitlement.FeatureTemplateMarket) {
		return nil, appErrs.Forbidden("paket Anda tidak mendukung template marketplace")
	}
	if plan == "starter" {
		return nil, appErrs.Forbidden("upgrade ke Business untuk membeli template pihak ketiga")
	}

	slug = strings.TrimSpace(slug)
	var listingID, kind string
	var price int
	var developerID sql.NullString
	err := sysDB.QueryRow(ctx, `
		SELECT tl.id::text, tl.kind, tl.price_idr, da.id::text
		FROM template_listing tl
		LEFT JOIN developer_account da ON da.id = tl.developer_id
		WHERE tl.slug = $1 AND tl.status = 'published'`, slug).
		Scan(&listingID, &kind, &price, &developerID)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("template tidak ditemukan")
	}
	if err != nil {
		return nil, err
	}
	if price <= 0 {
		return nil, appErrs.BadRequest("template gratis — pasang langsung via install")
	}

	var versionID string
	err = sysDB.QueryRow(ctx, `
		SELECT id::text FROM template_version
		WHERE listing_id = $1::uuid AND status = 'published'
		ORDER BY created_at DESC LIMIT 1`, listingID).Scan(&versionID)
	if err != nil {
		return nil, err
	}

	platformFee := price * platformFeePercent / 100
	devShare := price - platformFee

	var purchaseID string
	midtransOrder := fmt.Sprintf("WB-TPL-%s-%d", listingID[:8], time.Now().UnixMilli())
	err = sysDB.QueryRow(ctx, `
		INSERT INTO template_purchase
		(tenant_id, listing_id, version_id, amount_idr, platform_fee_idr, developer_share_idr,
		 midtrans_order_id, status, purchased_by)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, 'pending', $8::uuid)
		RETURNING id::text`,
		u.TenantID, listingID, versionID, price, platformFee, devShare, midtransOrder, u.AccountID).
		Scan(&purchaseID)
	if err != nil {
		return nil, err
	}

	qr, err := chargeTemplateQRIS(ctx, midtransOrder, int64(price), "Template: "+slug)
	if err != nil {
		return nil, appErrs.Unavailable("gagal membuat pembayaran")
	}

	if developerID.Valid {
		_, _ = sysDB.Exec(ctx, `
			INSERT INTO developer_payout_ledger (developer_id, purchase_id, amount_idr, status)
			VALUES ($1::uuid, $2::uuid, $3, 'pending')
			ON CONFLICT (purchase_id) DO NOTHING`,
			developerID.String, purchaseID, devShare)
	}

	return &PurchaseTemplateResponse{
		PurchaseID:    purchaseID,
		AmountIDR:     price,
		QRURL:         qr.QRURL,
		ExpiresAt:     qr.ExpiresAt,
		MidtransOrder: midtransOrder,
	}, nil
}

// FulfillTemplatePurchase marks purchase paid and installs template (idempotent).
func FulfillTemplatePurchase(ctx context.Context, midtransOrderID string) error {
	var purchaseID, tenantID, listingID, versionID, kind, status string
	err := sysDB.QueryRow(ctx, `
		SELECT tp.id::text, tp.tenant_id::text, tp.listing_id::text, tp.version_id::text, tl.kind, tp.status
		FROM template_purchase tp
		JOIN template_listing tl ON tl.id = tp.listing_id
		WHERE tp.midtrans_order_id = $1`, midtransOrderID).
		Scan(&purchaseID, &tenantID, &listingID, &versionID, &kind, &status)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if status == "paid" {
		return nil
	}

	_, err = sysDB.Exec(ctx, `UPDATE template_purchase SET status = 'paid' WHERE id = $1::uuid`, purchaseID)
	if err != nil {
		return err
	}

	var purchasedBy string
	if err := sysDB.QueryRow(ctx, `SELECT purchased_by::text FROM template_purchase WHERE id = $1::uuid`, purchaseID).Scan(&purchasedBy); err != nil {
		return err
	}
	if err := installTemplateSurfaces(ctx, tenantID, listingID, versionID, kind, purchasedBy); err != nil {
		return err
	}
	_, _ = sysDB.Exec(ctx, `
		UPDATE developer_payout_ledger SET status = 'accrued'
		WHERE purchase_id = $1::uuid AND status = 'pending'`, purchaseID)
	_, _ = sysDB.Exec(ctx, `UPDATE template_listing SET install_count = install_count + 1 WHERE id = $1::uuid`, listingID)
	return nil
}
