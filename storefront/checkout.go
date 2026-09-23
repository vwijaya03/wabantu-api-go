package storefront

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/payment"
	"encore.app/wabantu/shared/publictenant"
)

type ShippingAddress struct {
	Street     string `json:"street"`
	City       string `json:"city"`
	Province   string `json:"province"`
	PostalCode string `json:"postalCode"`
}

type CheckoutRequest struct {
	GuestName       string          `json:"guestName"`
	GuestPhone      string          `json:"guestPhone"`
	ShippingAddress ShippingAddress `json:"shippingAddress"`
}

type CheckoutPayment struct {
	QRURL     string    `json:"qrUrl"`
	ExpiresAt time.Time `json:"expiresAt"`
	StatusURL string    `json:"statusUrl,omitempty"`
}

type CheckoutResponse struct {
	OrderID string          `json:"orderId"`
	Total   float64         `json:"total"`
	Payment CheckoutPayment `json:"payment"`
}

type orderLine struct {
	LineID        string  `json:"lineId"`
	CatalogItemID string  `json:"catalogItemId"`
	Name          string  `json:"name"`
	Qty           float64 `json:"qty"`
	UnitPrice     float64 `json:"unitPrice"`
}

//encore:api public method=POST path=/api/v1/public/store/:tenantSlug/cart/:cartToken/checkout
func CheckoutPublicCart(ctx context.Context, tenantSlug, cartToken string, req *CheckoutRequest) (*CheckoutResponse, error) {
	return publictenant.RunPublicTenant(ctx, tenantSlug, resolveTenantBySlug, func(ref publictenant.TenantRef) (*CheckoutResponse, error) {
		if strings.TrimSpace(req.GuestName) == "" || strings.TrimSpace(req.GuestPhone) == "" {
			return nil, appErrs.BadRequest("nama dan telepon wajib")
		}
		if strings.TrimSpace(req.ShippingAddress.Street) == "" {
			return nil, appErrs.BadRequest("alamat pengiriman wajib")
		}

		ts, err := openTenant(ctx, ref.TenantSchema)
		if err != nil {
			return nil, err
		}
		var enabled bool
		err = ts.QueryRowContext(ctx, `SELECT is_enabled FROM `+ts.T("storefront_config")+` LIMIT 1`).Scan(&enabled)
		if err == sql.ErrNoRows || !enabled {
			return nil, appErrs.Unavailable("toko belum diaktifkan")
		}
		if err != nil {
			return nil, err
		}

		cart, err := loadCart(ctx, ref.TenantID, cartToken)
		if err != nil {
			return nil, err
		}
		if len(cart.Items) == 0 {
			return nil, appErrs.BadRequest("keranjang kosong")
		}

		lines := make([]orderLine, 0, len(cart.Items))
		var subtotal float64
		for _, ln := range cart.Items {
			var name string
			var price float64
			err = ts.QueryRowContext(ctx, `
				SELECT name, sell_price FROM `+ts.T("business_catalog_item")+`
				WHERE id = $1::uuid AND deleted_at IS NULL AND is_storefront_visible = true AND is_active = true`,
				ln.ProductID).Scan(&name, &price)
			if err != nil {
				return nil, appErrs.BadRequest("produk tidak tersedia: " + ln.ProductID)
			}
			qty := float64(ln.Qty)
			subtotal += qty * price
			lines = append(lines, orderLine{
				LineID: ln.LineID, CatalogItemID: ln.ProductID, Name: name, Qty: qty, UnitPrice: price,
			})
		}

		addr := map[string]string{
			"name": req.GuestName, "phone": req.GuestPhone,
			"street": req.ShippingAddress.Street, "city": req.ShippingAddress.City,
			"province": req.ShippingAddress.Province, "postalCode": req.ShippingAddress.PostalCode,
		}
		addrJSON, _ := json.Marshal(addr)
		itemsJSON, _ := json.Marshal(lines)

		var orderID string
		err = ts.QueryRowContext(ctx, `
			INSERT INTO `+ts.T("order")+`
			(items, shipping_address, status, subtotal, shipping_cost, total, source, payment_status, notes)
			VALUES ($1::jsonb, $2::jsonb, 'confirmed', $3, 0, $3, 'storefront', 'unpaid', 'Guest storefront checkout')
			RETURNING id::text`, itemsJSON, addrJSON, subtotal).Scan(&orderID)
		if err != nil {
			return nil, err
		}

		midtransOrderID := fmt.Sprintf("WB-SF-%s-%d", orderID, time.Now().UnixMilli())
		amountIDR := int64(subtotal)
		if amountIDR <= 0 {
			return nil, appErrs.BadRequest("total tidak valid")
		}

		qr, err := payment.ChargeQRIS(ctx, &payment.ChargeQRISRequest{
			TenantSchema: ref.TenantSchema,
			OrderID:      midtransOrderID,
			AmountIDR:    amountIDR,
			Description:  "Pesanan toko online",
		})
		if err != nil {
			return nil, appErrs.Unavailable("gagal membuat pembayaran: " + err.Error())
		}

		_, _ = ts.ExecContext(ctx, `
			UPDATE `+ts.T("order")+` SET payment_transaction_id = (
				SELECT id FROM `+ts.T("payment_transaction")+` WHERE midtrans_order_id = $2 LIMIT 1
			), updated_at = now() WHERE id = $1::uuid`, orderID, midtransOrderID)

		return &CheckoutResponse{
			OrderID: orderID,
			Total:   subtotal,
			Payment: CheckoutPayment{
				QRURL:     qr.QRURL,
				ExpiresAt: qr.ExpiresAt,
			},
		}, nil
	})
}
