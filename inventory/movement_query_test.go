package inventory

import (
	"strings"
	"testing"
)

func TestBuildMovementListWhereRequiresCatalogItemOnly(t *testing.T) {
	where, args, idx := buildMovementListWhere(&ListMovementsParams{
		CatalogItemID: "item-uuid",
		WarehouseID:   "wh-uuid",
		Type:          "opening_balance",
		Q:             "WOPN",
	})
	if !strings.Contains(where, "m.catalog_item_id = $1") {
		t.Fatalf("catalog filter missing: %s", where)
	}
	if strings.Contains(where, "COALESCE(ci.name") {
		t.Fatal("product search must use EXISTS, not bare ci join in outer WHERE")
	}
	if !strings.Contains(where, "EXISTS (SELECT 1 FROM business_catalog_item ci") {
		t.Fatal("missing EXISTS product name filter")
	}
	if len(args) != 4 {
		t.Fatalf("args len = %d, want 4", len(args))
	}
	if idx != 5 {
		t.Fatalf("next idx = %d, want 5", idx)
	}
}

func TestValidateListMovementsParams(t *testing.T) {
	if err := validateListMovementsParams(&ListMovementsParams{}); err == nil {
		t.Fatal("empty catalogItemId should fail")
	}
	if err := validateListMovementsParams(&ListMovementsParams{CatalogItemID: "x"}); err != nil {
		t.Fatalf("valid params: %v", err)
	}
}

func TestBuildMovementListWhereEmptyCatalog(t *testing.T) {
	where, args, idx := buildMovementListWhere(&ListMovementsParams{})
	if where != "WHERE 1=1" {
		t.Fatalf("where = %q", where)
	}
	if len(args) != 0 {
		t.Fatalf("args = %v", args)
	}
	if idx != 1 {
		t.Fatalf("idx = %d", idx)
	}
}
