package ai

import "testing"

func TestResolveSalesIntent_consultingNotCart(t *testing.T) {
	profile := &dbBusinessProfile{BusinessName: "Omah Apparel"}
	catalog := []dbCatalogItem{{
		Name: "[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT - L", SellPrice: 56900,
	}}
	history := []dbMessage{{Direction: "out", Body: "Produk boxer mono spot\nRp56900/paket"}}
	intent := ResolveSalesIntent("boleh beli 1 pcs ?", history, false, true, profile, catalog)
	if intent.State != SalesStateConsulting || intent.Topic != SalesTopicRetailPolicy {
		t.Fatalf("unexpected intent: %+v", intent)
	}
	if HasPurchaseIntent("boleh beli 1 pcs ?") {
		t.Fatal("should not be cart ready")
	}
}

func TestResolveSalesIntent_cartReady(t *testing.T) {
	profile := &dbBusinessProfile{BusinessName: "Omah Apparel"}
	catalog := []dbCatalogItem{{Name: "Jeans Katun", SellPrice: 150000}}
	intent := ResolveSalesIntent("saya jadi beli jeans katun 1 pcs", nil, false, true, profile, catalog)
	if intent.State != SalesStateCartReady {
		t.Fatalf("expected cart_ready, got %+v", intent)
	}
}

func TestResolveSalesIntent_explicitNewOrderStart(t *testing.T) {
	profile := &dbBusinessProfile{BusinessName: "Omah Apparel"}
	catalog := []dbCatalogItem{{
		Name: "[3 PCS] CELANA DALAM L XL PRIA COWOK DE WASA", SellPrice: 42200,
	}}
	history := []dbMessage{{Direction: "out", Body: "DE WASA harga Rp42200/paket"}}
	for _, msg := range []string{
		"mau buat pesanan baru bisa ?",
		"loh, saya mau buat pesanan baru oi",
	} {
		intent := ResolveSalesIntent(msg, history, false, true, profile, catalog)
		if intent.State != SalesStateCartReady {
			t.Fatalf("ResolveSalesIntent(%q) state=%s want cart_ready", msg, intent.State)
		}
	}
}

func TestResolveSalesIntent_browsing(t *testing.T) {
	profile := &dbBusinessProfile{BusinessName: "Toko"}
	intent := ResolveSalesIntent("di toko ini tersedia jualan apa saja ya ?", nil, false, true, profile, nil)
	if intent.State != SalesStateBrowsing {
		t.Fatalf("expected browsing, got %+v", intent)
	}
}
