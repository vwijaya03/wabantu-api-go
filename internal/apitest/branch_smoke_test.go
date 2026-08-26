package apitest

import (
	"testing"

	"encore.app/wabantu/branch"
)

func TestBranchSmoke_ListBranches(t *testing.T) {
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)

	resp, err := branch.ListBranches(t.Context())
	if err != nil {
		t.Fatalf("GET /api/v1/branches: %v", err)
	}
	AssertJSONFields(t, resp, "branches")
	AssertJSONArrayField(t, resp, "branches")
}
