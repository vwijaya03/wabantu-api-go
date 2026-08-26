package apitest_test

import (
	"context"
	"testing"

	"encore.app/wabantu/internal/apitest"
	"encore.app/wabantu/order"
)

func TestOrderListSmoke(t *testing.T) {
	fx := apitest.BootstrapOwner(t)
	apitest.WithOwnerAuth(fx)

	resp, err := order.List(context.Background(), &order.ListOrdersParams{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("GET /api/v1/orders (List): %v", err)
	}
	if resp == nil {
		t.Fatal("nil ListOrdersResponse")
	}
	if resp.Orders == nil {
		t.Fatal("orders slice is nil")
	}
	if len(resp.Orders) != 0 {
		t.Fatalf("expected empty orders, got %d", len(resp.Orders))
	}
	if resp.Total != 0 {
		t.Fatalf("expected total=0, got %d", resp.Total)
	}
}

func TestOrderCreateSmoke(t *testing.T) {
	fx := apitest.BootstrapOwner(t)
	apitest.WithOwnerAuth(fx)

	created, err := order.Create(context.Background(), &order.CreateOrderParams{
		Items: []order.OrderItem{{
			Name:      "Smoke Manual Item",
			Qty:       1,
			UnitPrice: 15000,
		}},
	})
	if err != nil {
		t.Fatalf("POST /api/v1/orders (Create): %v", err)
	}
	if created == nil || created.ID == "" {
		t.Fatalf("expected created order id, got %+v", created)
	}
	if created.Status != "draft" {
		t.Fatalf("expected draft status, got %q", created.Status)
	}

	listed, err := order.List(context.Background(), &order.ListOrdersParams{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("GET /api/v1/orders after create: %v", err)
	}
	if listed.Total < 1 {
		t.Fatalf("expected total >= 1 after create, got %d", listed.Total)
	}
}
