package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// L-2 (SECURITY_REVIEW): LLM tool arguments must not be able to carry or override the
// tenant/schema. The catalog executor is built per job from the tenant's own catalog
// snapshot (ai/autoreply.go NewCatalogToolExecutor(catalog)) and holds no DB handle, so
// whatever the model puts into the arguments only filters that snapshot.

var tenantKeys = []string{"tenantid", "tenant_id", "tenant", "schema", "tenantschema", "tenant_schema", "search_path"}

func TestToolDefsDeclareNoTenantOrSchemaParam(t *testing.T) {
	defs := catalogToolDefsNeutral()
	if len(defs) != len(catalogToolDefinitions()) || len(defs) == 0 {
		t.Fatalf("neutral defs=%d", len(defs))
	}
	for _, d := range defs {
		props, _ := d.Parameters["properties"].(map[string]any)
		for k := range props {
			lk := strings.ToLower(k)
			for _, bad := range tenantKeys {
				if lk == bad || strings.Contains(lk, "tenant") || strings.Contains(lk, "schema") {
					t.Fatalf("tool %s declares %q (%s)", d.Name, k, bad)
				}
			}
		}
	}
	for _, d := range catalogToolDefinitions() {
		for k := range d.OfTool.InputSchema.Properties.(map[string]any) {
			if strings.Contains(strings.ToLower(k), "tenant") || strings.Contains(strings.ToLower(k), "schema") {
				t.Fatalf("anthropic tool %s declares %q", d.OfTool.Name, k)
			}
		}
	}
}

func TestCatalogExecutorIgnoresInjectedTenantArgs(t *testing.T) {
	tenantA := []dbCatalogItem{{ExternalCode: "A-1", Name: "Kopi Gayo Tenant A", SellPrice: 85000, SellUnit: "pcs"}}
	tenantB := []dbCatalogItem{{ExternalCode: "B-1", Name: "Kopi Gayo Tenant B", SellPrice: 1000, SellUnit: "pcs"}}
	execA := NewCatalogToolExecutor(tenantA)
	_ = NewCatalogToolExecutor(tenantB)

	inject := `"tenantId":"tenant-b","tenant_id":"tenant-b","schema":"tenant_b","tenantSchema":"tenant_b","tenant_schema":"tenant_b","search_path":"tenant_b,public"`
	cases := []struct{ tool, args string }{
		{catalogToolSearch, `{"query":"kopi gayo",` + inject + `}`},
		{catalogToolSearch, `{"query":"tenant b",` + inject + `,"limit":10}`},
		{catalogToolGetProduct, `{"ref":"B-1",` + inject + `}`},
		{catalogToolGetProduct, `{"ref":"Kopi Gayo Tenant B",` + inject + `}`},
	}
	for _, c := range cases {
		out, err := execA.Run(c.tool, json.RawMessage(c.args))
		if err != nil {
			t.Fatalf("%s %s: %v", c.tool, c.args, err)
		}
		if strings.Contains(out, "B-1") || strings.Contains(out, "1.000") || strings.Contains(out, "Rp1000") {
			t.Fatalf("tenant B data leaked via %s: %s", c.tool, out)
		}
		if strings.Contains(out, `"found":true`) && !strings.Contains(out, "Tenant A") {
			t.Fatalf("unexpected result: %s", out)
		}
	}
	// Positive control: the same executor still answers from its own snapshot.
	out, _ := execA.Run(catalogToolGetProduct, json.RawMessage(`{"ref":"A-1","tenantId":"tenant-b"}`))
	if !strings.Contains(out, "Tenant A") {
		t.Fatalf("own product not found: %s", out)
	}
}

func TestCatalogExecutorRejectsUnknownAndMalformedTools(t *testing.T) {
	e := NewCatalogToolExecutor([]dbCatalogItem{{Name: "X"}})
	for _, name := range []string{"set_tenant", "sql", "update_price", "set_order_status", ""} {
		if _, err := e.Run(name, json.RawMessage(`{"tenantId":"x"}`)); err == nil {
			t.Fatalf("tool %q accepted", name)
		}
	}
	if _, err := e.Run(catalogToolSearch, json.RawMessage(`{"query":1}`)); err == nil {
		t.Fatal("malformed args accepted")
	}
	var nilExec *CatalogToolExecutor
	if _, err := nilExec.Run(catalogToolSearch, json.RawMessage(`{}`)); err == nil {
		t.Fatal("nil executor accepted")
	}
}
