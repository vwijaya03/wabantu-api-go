package payment

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
)

var dataDB = sqldb.Named("tenant")
var systemDB = sqldb.Named("system")

var secrets struct {
	MidtransServerKey    string
	MidtransClientKey    string
	MidtransIsProduction string
}

// ---------- types ----------

type CreateQRISParams struct {
	InvoiceID   string `json:"invoiceId"`
	AmountIDR   int64  `json:"amountIdr"`
	Description string `json:"description"`
}

type QRISResponse struct {
	TransactionID string    `json:"transactionId"`
	OrderID       string    `json:"orderId"`
	QRURL         string    `json:"qrUrl"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

type PaymentStatus struct {
	ID              string     `json:"id"`
	OrderID         string     `json:"orderId"`
	TransactionID   string     `json:"transactionId"`
	InvoiceID       string     `json:"invoiceId"`
	AmountIDR       int64      `json:"amountIdr"`
	Status          string     `json:"status"`
	PaymentType     string     `json:"paymentType"`
	QRURL           string     `json:"qrUrl"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty"`
	PaidAt          *time.Time `json:"paidAt,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
}

type midtransChargeReq struct {
	PaymentType string                    `json:"payment_type"`
	Transaction midtransTransactionDetail `json:"transaction_details"`
}

type midtransTransactionDetail struct {
	OrderID     string `json:"order_id"`
	GrossAmount int64  `json:"gross_amount"`
}

type midtransChargeResp struct {
	StatusCode    string `json:"status_code"`
	StatusMessage string `json:"status_message"`
	TransactionID string `json:"transaction_id"`
	OrderID       string `json:"order_id"`
	GrossAmount   string `json:"gross_amount"`
	PaymentType   string `json:"payment_type"`
	TransStatus   string `json:"transaction_status"`
	Actions       []struct {
		Name   string `json:"name"`
		Method string `json:"method"`
		URL    string `json:"url"`
	} `json:"actions"`
}

type midtransNotification struct {
	TransactionID   string `json:"transaction_id"`
	OrderID         string `json:"order_id"`
	StatusCode      string `json:"status_code"`
	GrossAmount     string `json:"gross_amount"`
	SignatureKey    string `json:"signature_key"`
	TransStatus     string `json:"transaction_status"`
	FraudStatus     string `json:"fraud_status"`
	PaymentType     string `json:"payment_type"`
}

// ---------- endpoints ----------

//encore:api auth method=POST path=/api/v1/payment/create-qris
func CreateQRIS(ctx context.Context, p *CreateQRISParams) (*QRISResponse, error) {
	u, _ := auth.Data().(*types.AuthUser)
	if u == nil || u.Role != "owner" {
		return nil, appErrs.Forbidden("owner access required")
	}
	if p.AmountIDR <= 0 {
		return nil, appErrs.BadRequest("amountIdr must be positive")
	}

	orderID := fmt.Sprintf("WB-%s-%d", p.InvoiceID, time.Now().UnixMilli())

	chargeResp, err := callMidtransCharge(orderID, p.AmountIDR)
	if err != nil {
		return nil, appErrs.Unavailable("payment gateway error: " + err.Error())
	}

	qrURL := ""
	for _, a := range chargeResp.Actions {
		if a.Name == "generate-qr-code" {
			qrURL = a.URL
			break
		}
	}

	expiresAt := time.Now().Add(15 * time.Minute)

	_, err = dataDB.Exec(ctx, fmt.Sprintf(
		`INSERT INTO "%s".payment_transaction
			(midtrans_order_id, midtrans_transaction_id, invoice_id, amount_idr,
			 description, status, payment_type, qr_url, expires_at)
		 VALUES ($1,$2,$3,$4,$5,'PENDING','qris',$6,$7)`,
		u.TenantSchema),
		orderID, chargeResp.TransactionID, p.InvoiceID, p.AmountIDR,
		p.Description, qrURL, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("save transaction: %w", err)
	}

	_, err = systemDB.Exec(ctx,
		`INSERT INTO payment_webhook_map (order_id, tenant_schema) VALUES ($1,$2)`,
		orderID, u.TenantSchema)
	if err != nil {
		rlog.Error("failed to save webhook map", "err", err)
	}

	return &QRISResponse{
		TransactionID: chargeResp.TransactionID,
		OrderID:       orderID,
		QRURL:         qrURL,
		ExpiresAt:     expiresAt,
	}, nil
}

//encore:api auth method=GET path=/api/v1/payment/:id/status
func GetStatus(ctx context.Context, id string) (*PaymentStatus, error) {
	u, _ := auth.Data().(*types.AuthUser)
	if u == nil {
		return nil, appErrs.Unauthenticated("missing auth data")
	}

	var ps PaymentStatus
	err := dataDB.QueryRow(ctx, fmt.Sprintf(
		`SELECT id, midtrans_order_id, COALESCE(midtrans_transaction_id,''),
		        COALESCE(invoice_id,''), amount_idr, status, payment_type,
		        COALESCE(qr_url,''), expires_at, paid_at, created_at
		 FROM "%s".payment_transaction WHERE id=$1 AND deleted_at IS NULL`,
		u.TenantSchema), id,
	).Scan(&ps.ID, &ps.OrderID, &ps.TransactionID,
		&ps.InvoiceID, &ps.AmountIDR, &ps.Status, &ps.PaymentType,
		&ps.QRURL, &ps.ExpiresAt, &ps.PaidAt, &ps.CreatedAt)
	if err != nil {
		return nil, appErrs.NotFound("payment transaction not found")
	}
	return &ps, nil
}

//encore:api public raw method=POST path=/api/v1/payment/webhook/midtrans
func MidtransWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var n midtransNotification
	if err := json.Unmarshal(body, &n); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	raw := n.OrderID + n.StatusCode + n.GrossAmount + secrets.MidtransServerKey
	hash := sha512.Sum512([]byte(raw))
	expected := hex.EncodeToString(hash[:])
	if expected != n.SignatureKey {
		rlog.Warn("midtrans webhook signature mismatch", "orderId", n.OrderID)
		w.WriteHeader(http.StatusForbidden)
		return
	}

	ctx := r.Context()

	var tenantSchema string
	if err := systemDB.QueryRow(ctx,
		`SELECT tenant_schema FROM payment_webhook_map WHERE order_id=$1`,
		n.OrderID).Scan(&tenantSchema); err != nil {
		rlog.Error("webhook map lookup failed", "orderId", n.OrderID, "err", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	var currentStatus string
	if err := dataDB.QueryRow(ctx, fmt.Sprintf(
		`SELECT status FROM "%s".payment_transaction WHERE midtrans_order_id=$1`,
		tenantSchema), n.OrderID).Scan(&currentStatus); err != nil {
		rlog.Error("transaction lookup failed", "orderId", n.OrderID, "err", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	if currentStatus != "PENDING" {
		w.WriteHeader(http.StatusOK)
		return
	}

	newStatus := mapMidtransStatus(n.TransStatus)
	if newStatus == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if newStatus == "PAID" {
		_, _ = dataDB.Exec(ctx, fmt.Sprintf(
			`UPDATE "%s".payment_transaction SET status=$1, paid_at=NOW(), updated_at=NOW()
			 WHERE midtrans_order_id=$2`, tenantSchema), newStatus, n.OrderID)
		_, _ = dataDB.Exec(ctx, fmt.Sprintf(
			`UPDATE "%s"."order" SET status='paid', updated_at=NOW()
			 WHERE payment_transaction_id = (
			   SELECT id FROM "%s".payment_transaction WHERE midtrans_order_id=$1 LIMIT 1
			 ) AND status IN ('draft','confirmed')`, tenantSchema, tenantSchema), n.OrderID)
	} else {
		_, _ = dataDB.Exec(ctx, fmt.Sprintf(
			`UPDATE "%s".payment_transaction SET status=$1, updated_at=NOW()
			 WHERE midtrans_order_id=$2`, tenantSchema), newStatus, n.OrderID)
	}

	rlog.Info("payment status updated", "orderId", n.OrderID, "status", newStatus)
	w.WriteHeader(http.StatusOK)
}

// ---------- internal ----------

func midtransBaseURL() string {
	if secrets.MidtransIsProduction == "true" {
		return "https://api.midtrans.com"
	}
	return "https://api.sandbox.midtrans.com"
}

func callMidtransCharge(orderID string, amount int64) (*midtransChargeResp, error) {
	payload := midtransChargeReq{
		PaymentType: "qris",
		Transaction: midtransTransactionDetail{OrderID: orderID, GrossAmount: amount},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", midtransBaseURL()+"/v2/charge", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(secrets.MidtransServerKey, "")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("midtrans request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result midtransChargeResp
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("midtrans response parse error: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("midtrans returned %d: %s", resp.StatusCode, result.StatusMessage)
	}
	return &result, nil
}

func mapMidtransStatus(transStatus string) string {
	switch transStatus {
	case "settlement", "capture":
		return "PAID"
	case "expire":
		return "EXPIRED"
	case "deny", "cancel":
		return "FAILED"
	default:
		return ""
	}
}
