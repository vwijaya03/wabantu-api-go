package webhook

import "net/http"

// ServeWhatsAppWebhookHTTP runs /api/v1/webhook/whatsapp (integration smoke tests).
func ServeWhatsAppWebhookHTTP(w http.ResponseWriter, req *http.Request) {
	handleWhatsAppWebhook(w, req)
}
