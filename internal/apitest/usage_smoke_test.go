package apitest

import (
	"testing"

	"encore.app/wabantu/usage"
)

func TestUsageSmoke_Summary(t *testing.T) {
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)

	resp, err := usage.Summary(t.Context(), &usage.SummaryParams{})
	if err != nil {
		t.Fatalf("GET /api/v1/usage/summary: %v", err)
	}
	AssertJSONFields(t, resp, "period", "plan", "quotas")
	AssertJSONArrayField(t, resp, "quotas")
}
