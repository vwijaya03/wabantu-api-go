package apitest

import (
	"testing"

	"encore.app/wabantu/kb"
)

func TestKBSmoke_List(t *testing.T) {
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)

	resp, err := kb.List(t.Context(), &kb.ListRequest{})
	if err != nil {
		t.Fatalf("GET /api/v1/knowledge-base: %v", err)
	}
	AssertJSONFields(t, resp, "items", "total", "page", "pageSize")
	AssertJSONArrayField(t, resp, "items")
}
