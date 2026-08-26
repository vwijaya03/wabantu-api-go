package apitest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"encore.app/wabantu/payment"
)

func TestPaymentWebhookSmoke_Midtrans_InvalidJSON(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/payment/webhook/midtrans", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	payment.ServeMidtransWebhookHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON status = %d, want %d; body: %s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
}

func TestPaymentWebhookSmoke_Midtrans_MissingSignature(t *testing.T) {
	rr := httptest.NewRecorder()
	req := NewJSONPostRequest(t, "/api/v1/payment/webhook/midtrans", map[string]string{
		"order_id":           "apitest-order",
		"status_code":        "200",
		"gross_amount":       "10000.00",
		"transaction_status": "settlement",
	})
	payment.ServeMidtransWebhookHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("missing signature status = %d, want %d; body: %s", rr.Code, http.StatusForbidden, rr.Body.String())
	}
}
