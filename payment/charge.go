package payment

import (
	"context"
	"fmt"
	"time"

	"encore.dev/rlog"
	"encore.dev/storage/sqldb"
)

var chargeDataDB = sqldb.Named("tenant")
var chargeSystemDB = sqldb.Named("system")

// ChargeQRISRequest creates a QRIS payment for storefront orders or other tenant-scoped charges.
type ChargeQRISRequest struct {
	TenantSchema string
	OrderID      string
	AmountIDR    int64
	Description  string
	InvoiceID    string // optional — empty for storefront guest checkout
}

// ChargeQRIS charges Midtrans and persists payment_transaction in the tenant schema.
func ChargeQRIS(ctx context.Context, req *ChargeQRISRequest) (*QRISResponse, error) {
	if req.AmountIDR <= 0 {
		return nil, fmt.Errorf("amount must be positive")
	}
	if req.TenantSchema == "" || req.OrderID == "" {
		return nil, fmt.Errorf("tenant schema and order id required")
	}

	chargeResp, err := callMidtransCharge(req.OrderID, req.AmountIDR)
	if err != nil {
		return nil, err
	}

	qrURL := ""
	for _, a := range chargeResp.Actions {
		if a.Name == "generate-qr-code" {
			qrURL = a.URL
			break
		}
	}

	expiresAt := time.Now().Add(15 * time.Minute)
	invoiceID := nullUUIDArg(req.InvoiceID)

	_, err = chargeDataDB.Exec(ctx, fmt.Sprintf(
		`INSERT INTO "%s".payment_transaction
			(midtrans_order_id, midtrans_transaction_id, invoice_id, amount_idr,
			 description, status, payment_type, qr_url, expires_at)
		 VALUES ($1,$2,$3,$4,$5,'PENDING','qris',$6,$7)`,
		req.TenantSchema),
		req.OrderID, chargeResp.TransactionID, invoiceID, req.AmountIDR,
		req.Description, qrURL, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("save transaction: %w", err)
	}

	_, err = chargeSystemDB.Exec(ctx,
		`INSERT INTO payment_webhook_map (order_id, tenant_schema) VALUES ($1,$2)
		 ON CONFLICT (order_id) DO NOTHING`,
		req.OrderID, req.TenantSchema)
	if err != nil {
		rlog.Error("failed to save webhook map", "err", err)
	}

	return &QRISResponse{
		TransactionID: chargeResp.TransactionID,
		OrderID:       req.OrderID,
		QRURL:         qrURL,
		ExpiresAt:     expiresAt,
	}, nil
}

func nullUUIDArg(s string) any {
	if s == "" {
		return nil
	}
	return s
}
