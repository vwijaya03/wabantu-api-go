package tenant

import (
	"strings"
	"testing"
)

// TestCloudAdminDDLCoversCloudTenantReady documents that every CloudTenantReady check
// has a corresponding block in cloudAdminTenantDDLBlocks — update when adding new modules.
func TestCloudAdminDDLCoversCloudTenantReady(t *testing.T) {
	blocks := cloudAdminTenantDDLBlocks()
	if len(blocks) == 0 {
		t.Fatal("cloudAdminTenantDDLBlocks is empty")
	}

	required := []string{
		"TenantPatchReady",
		"PricingReady",
		"KnowledgeBaseReady",
		"FinanceModuleReady",
		"EventsModuleReady",
		"PIIReady",
		"OrderIncomePatchReady",
		"OrderPaymentProofPatchReady",
		"InventoryModuleReady",
	}

	covered := strings.Join(func() []string {
		out := make([]string, len(blocks))
		for i, b := range blocks {
			out[i] = b.covers
		}
		return out
	}(), " ")

	for _, check := range required {
		if !strings.Contains(covered, check) {
			t.Errorf("CloudTenantReady check %q not documented in cloudAdminTenantDDLBlocks covers", check)
		}
	}

	seen := make(map[string]struct{}, len(blocks))
	for _, b := range blocks {
		if strings.TrimSpace(b.sql) == "" {
			t.Errorf("block %q has empty SQL", b.label)
		}
		if _, ok := seen[b.label]; ok {
			t.Errorf("duplicate block label %q", b.label)
		}
		seen[b.label] = struct{}{}
	}
}
