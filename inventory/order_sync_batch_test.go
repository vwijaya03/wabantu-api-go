package inventory

import "testing"

func TestResolveOrderRequirementsWithCache(t *testing.T) {
	cache := &skuBundleCache{
		sku: map[string]skuMeta{
			"a": {exists: true, trackStock: true},
			"b": {exists: true, trackStock: true},
			"bundle": {exists: true, isBundle: true},
		},
		bundles: map[string][]BundleComponent{
			"bundle": {{ChildCatalogItemID: "child", Qty: 2}},
		},
	}
	got := resolveOrderRequirementsWithCache(cache, []OrderStockItem{
		{CatalogItemID: "a", WarehouseID: "w1", Qty: 3},
		{CatalogItemID: "a", WarehouseID: "w1", Qty: 2},
		{CatalogItemID: "b", WarehouseID: "", Qty: 1},
		{CatalogItemID: "bundle", WarehouseID: "w1", Qty: 4},
		{CatalogItemID: "missing", WarehouseID: "w1", Qty: 9},
	}, "w-default")
	if !approx(got[reqKey{item: "a", warehouse: "w1"}], 5) {
		t.Fatalf("a/w1 = %v, want 5", got[reqKey{item: "a", warehouse: "w1"}])
	}
	if !approx(got[reqKey{item: "b", warehouse: "w-default"}], 1) {
		t.Fatalf("b/default = %v, want 1", got[reqKey{item: "b", warehouse: "w-default"}])
	}
	if !approx(got[reqKey{item: "child", warehouse: "w1"}], 8) {
		t.Fatalf("bundle child = %v, want 8", got[reqKey{item: "child", warehouse: "w1"}])
	}
	if _, ok := got[reqKey{item: "missing", warehouse: "w1"}]; ok {
		t.Fatal("missing sku should be skipped")
	}
}

func TestCollectCatalogIDsFromOrders(t *testing.T) {
	got := collectCatalogIDsFromOrders(
		[]OrderStockItem{{CatalogItemID: "a"}, {CatalogItemID: "a"}, {CatalogItemID: ""}},
		[]OrderStockItem{{CatalogItemID: "b"}},
	)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}

func TestOrderStockSyncDeltaBatch(t *testing.T) {
	k := reqKey{item: "a", warehouse: "w1"}
	required := map[reqKey]float64{k: 5}
	net := map[reqKey]netEntry{k: {qty: 3}}
	if !orderStockSyncDelta(required, net) {
		t.Fatal("expected needs sync")
	}
	net[k] = netEntry{qty: 5}
	if orderStockSyncDelta(required, net) {
		t.Fatal("expected synced")
	}
}
