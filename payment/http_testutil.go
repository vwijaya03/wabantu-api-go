package payment

import "net/http"

// ServeMidtransWebhookHTTP runs POST /api/v1/payment/webhook/midtrans (integration smoke tests).
func ServeMidtransWebhookHTTP(w http.ResponseWriter, req *http.Request) {
	serveMidtransWebhook(w, req)
}
