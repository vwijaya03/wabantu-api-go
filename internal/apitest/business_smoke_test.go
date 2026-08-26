package apitest

import (
	"testing"

	"encore.app/wabantu/business"
)

func TestBusinessSmoke_GetProfile(t *testing.T) {
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)

	resp, err := business.GetProfile(t.Context())
	if err != nil {
		t.Fatalf("GET /api/v1/business/profile: %v", err)
	}
	AssertJSONFields(t, resp, "profile")
}
