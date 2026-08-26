package apitest

import (
	"testing"

	"encore.app/wabantu/billing"
)

func TestBillingSmoke_Overview(t *testing.T) {
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)

	resp, err := billing.Overview(t.Context())
	if err != nil {
		t.Fatalf("GET /api/v1/billing/overview: %v", err)
	}
	AssertJSONFields(t, resp, "subscription", "plans", "topUpOptions", "invoices")
	AssertJSONArrayField(t, resp, "plans")
	AssertJSONArrayField(t, resp, "topUpOptions")
}
