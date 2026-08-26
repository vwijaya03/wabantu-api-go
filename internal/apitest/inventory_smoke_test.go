package apitest

import (
	"testing"

	"encore.app/wabantu/inventory"
)

func TestInventorySmoke_ListWarehouses(t *testing.T) {
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)

	resp, err := inventory.ListWarehouses(t.Context(), &inventory.ListWarehousesParams{})
	if err != nil {
		t.Fatalf("GET /api/v1/inventory/warehouses: %v", err)
	}
	AssertJSONFields(t, resp, "warehouses", "total")
	AssertJSONArrayField(t, resp, "warehouses")
}

func TestInventorySmoke_ListConfigItems(t *testing.T) {
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)

	resp, err := inventory.ListConfigItems(t.Context(), &inventory.ListConfigItemsParams{})
	if err != nil {
		t.Fatalf("GET /api/v1/inventory/config-items: %v", err)
	}
	AssertJSONFields(t, resp, "items", "total", "page", "pageSize")
	AssertJSONArrayField(t, resp, "items")
}
