package webhook

import "net/http"

// ServeWhatsAppWebhookHTTP runs /api/v1/webhook/whatsapp (integration smoke tests).
func ServeWhatsAppWebhookHTTP(w http.ResponseWriter, req *http.Request) {
	handleMetaWebhook(w, req)
}

// ServeMetaWebhookHTTP runs /api/v1/whatsapp/webhook/meta (integration smoke tests).
func ServeMetaWebhookHTTP(w http.ResponseWriter, req *http.Request) {
	handleMetaWebhook(w, req)
}

// ServeMetaWebhookLegacyHTTP runs /whatsapp/webhook/meta (integration smoke tests).
func ServeMetaWebhookLegacyHTTP(w http.ResponseWriter, req *http.Request) {
	handleMetaWebhook(w, req)
}

// ServeWhatsAppWebhookLegacyHTTP runs /webhook/whatsapp (integration smoke tests).
func ServeWhatsAppWebhookLegacyHTTP(w http.ResponseWriter, req *http.Request) {
	handleMetaWebhook(w, req)
}
