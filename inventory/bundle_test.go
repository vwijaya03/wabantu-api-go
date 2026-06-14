package inventory

import "testing"

func TestExplodeBundle(t *testing.T) {
	// Bundle "3 PCS" of one child + 1 of another, selling 2 bundles.
	comps := []BundleComponent{
		{ChildCatalogItemID: "a", Qty: 3},
		{ChildCatalogItemID: "b", Qty: 1},
	}
	issues := explodeBundle(comps, 2)
	if len(issues) != 2 {
		t.Fatalf("want 2 issues, got %d", len(issues))
	}
	if issues[0].CatalogItemID != "a" || !approx(issues[0].Qty, 6) {
		t.Fatalf("issue[0] = %+v, want a x6", issues[0])
	}
	if issues[1].CatalogItemID != "b" || !approx(issues[1].Qty, 2) {
		t.Fatalf("issue[1] = %+v, want b x2", issues[1])
	}
}

func TestExplodeBundleFractional(t *testing.T) {
	comps := []BundleComponent{{ChildCatalogItemID: "kg", Qty: 0.5}}
	issues := explodeBundle(comps, 3)
	if len(issues) != 1 || !approx(issues[0].Qty, 1.5) {
		t.Fatalf("issues = %+v, want 1.5", issues)
	}
}

func TestBundleAvailableQty(t *testing.T) {
	comps := []BundleComponent{
		{ChildCatalogItemID: "a", Qty: 3},
		{ChildCatalogItemID: "b", Qty: 1},
	}
	// a: 10 -> floor(10/3)=3 ; b: 5 -> floor(5/1)=5 ; min=3
	got := bundleAvailableQty(map[string]float64{"a": 10, "b": 5}, comps)
	if !approx(got, 3) {
		t.Fatalf("available = %v, want 3", got)
	}
	// missing child -> 0
	got = bundleAvailableQty(map[string]float64{"a": 10}, comps)
	if !approx(got, 0) {
		t.Fatalf("available with missing child = %v, want 0", got)
	}
	// empty components -> 0
	if bundleAvailableQty(map[string]float64{"a": 10}, nil) != 0 {
		t.Fatal("empty components should be 0")
	}
}

func TestValidateBundleComponents(t *testing.T) {
	ok := []BundleComponent{{ChildCatalogItemID: "a", Qty: 1}, {ChildCatalogItemID: "b", Qty: 2}}
	if err := validateBundleComponents("parent", ok); err != nil {
		t.Fatalf("valid components rejected: %v", err)
	}
	if err := validateBundleComponents("parent", nil); err == nil {
		t.Fatal("empty should error")
	}
	selfRef := []BundleComponent{{ChildCatalogItemID: "parent", Qty: 1}}
	if err := validateBundleComponents("parent", selfRef); err == nil {
		t.Fatal("self-reference should error")
	}
	dup := []BundleComponent{{ChildCatalogItemID: "a", Qty: 1}, {ChildCatalogItemID: "a", Qty: 2}}
	if err := validateBundleComponents("parent", dup); err == nil {
		t.Fatal("duplicate child should error")
	}
	badQty := []BundleComponent{{ChildCatalogItemID: "a", Qty: 0}}
	if err := validateBundleComponents("parent", badQty); err == nil {
		t.Fatal("zero qty should error")
	}
	empty := []BundleComponent{{ChildCatalogItemID: "  ", Qty: 1}}
	if err := validateBundleComponents("parent", empty); err == nil {
		t.Fatal("blank child should error")
	}
}
