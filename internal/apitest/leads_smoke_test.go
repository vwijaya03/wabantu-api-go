package apitest

import (
	"testing"

	"encore.app/wabantu/leads"
)

func TestLeadsSmoke_List(t *testing.T) {
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)

	resp, err := leads.List(t.Context(), &leads.ListRequest{})
	if err != nil {
		t.Fatalf("GET /api/v1/leads: %v", err)
	}
	AssertJSONFields(t, resp, "items")
	AssertJSONArrayField(t, resp, "items")
}
