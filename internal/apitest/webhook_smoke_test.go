package apitest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"encore.app/wabantu/webhook"
)

type webhookHandlerCase struct {
	name    string
	handler func(http.ResponseWriter, *http.Request)
}

func webhookHandlers() []webhookHandlerCase {
	return []webhookHandlerCase{
		{name: "ServeWhatsAppWebhookHTTP", handler: webhook.ServeWhatsAppWebhookHTTP},
		{name: "ServeMetaWebhookHTTP", handler: webhook.ServeMetaWebhookHTTP},
		{name: "ServeMetaWebhookLegacyHTTP", handler: webhook.ServeMetaWebhookLegacyHTTP},
		{name: "ServeWhatsAppWebhookLegacyHTTP", handler: webhook.ServeWhatsAppWebhookLegacyHTTP},
	}
}

func TestWebhookSmoke_HandlersExist(t *testing.T) {
	for _, tc := range webhookHandlers() {
		if tc.handler == nil {
			t.Fatalf("%s handler is nil", tc.name)
		}
	}
}

func TestWebhookSmoke_GET_VerifyChallengeForbidden(t *testing.T) {
	for _, tc := range webhookHandlers() {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := NewGetRequest("/api/v1/webhook/whatsapp?hub.mode=subscribe&hub.verify_token=wrong", nil)
			tc.handler(rr, req)
			if rr.Code != http.StatusForbidden {
				t.Fatalf("GET verify status = %d, want %d; body: %s", rr.Code, http.StatusForbidden, rr.Body.String())
			}
		})
	}
}

func TestWebhookSmoke_POST_MissingSignatureRejected(t *testing.T) {
	body := map[string]string{"object": "whatsapp_business_account"}
	for _, tc := range webhookHandlers() {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := NewJSONPostRequest(t, "/api/v1/webhook/whatsapp", body)
			tc.handler(rr, req)
			// Payload without phone_number_id and no signature is accepted (legacy path).
			if rr.Code != http.StatusOK {
				t.Fatalf("POST empty payload status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
			}
		})
	}
}

func TestWebhookSmoke_POST_PhoneNumberIDWithoutSignatureUnauthorized(t *testing.T) {
	body := map[string]any{
		"object": "whatsapp_business_account",
		"entry": []map[string]any{
			{
				"changes": []map[string]any{
					{
						"value": map[string]any{
							"metadata": map[string]string{
								"phone_number_id": "apitest-smoke-phone-id",
							},
						},
					},
				},
			},
		},
	}
	for _, tc := range webhookHandlers() {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := NewJSONPostRequest(t, "/api/v1/webhook/whatsapp", body)
			tc.handler(rr, req)
			if rr.Code != http.StatusUnauthorized {
				t.Fatalf("POST unsigned status = %d, want %d; body: %s", rr.Code, http.StatusUnauthorized, rr.Body.String())
			}
		})
	}
}

func TestWebhookSmoke_MethodNotAllowed(t *testing.T) {
	for _, tc := range webhookHandlers() {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, "/api/v1/webhook/whatsapp", nil)
			tc.handler(rr, req)
			if rr.Code != http.StatusMethodNotAllowed {
				t.Fatalf("PUT status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}
