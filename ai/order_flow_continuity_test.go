package ai

import (
	"strings"
	"testing"
)

func abonCatalog() []dbCatalogItem {
	return []dbCatalogItem{{
		ID: "abon-1", ExternalCode: "ABON", Name: "Abon Sapi 500G",
		SellPrice: 35000, SellUnit: "pcs",
		StockTracked: true, StockAvailable: 30,
		StockByWarehouse: stockLines(20, 10),
	}}
}

func boxerCatalog() []dbCatalogItem {
	return []dbCatalogItem{{
		ID: "boxer-3", ExternalCode: "BOXER-3",
		Name: "[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT - L",
		SellPrice: 56900, SellUnit: "pcs",
	}}
}

func mixedCatalog() []dbCatalogItem {
	return append(abonCatalog(), boxerCatalog()...)
}

func TestShouldBreakOrderFlow_namedProductAtAskProduct(t *testing.T) {
	catalog := abonCatalog()
	msg := "mau beli abon sapi yang 500 gram, stok ready ga ?"
	if ShouldBreakOrderFlow(msg, "ask_product", catalog) {
		t.Fatal("named product purchase should continue order flow at ask_product")
	}
}

func TestResolveSalesIntent_namedProductPurchase_cartReady(t *testing.T) {
	profile := &dbBusinessProfile{BusinessName: "Omah Apparel"}
	catalog := abonCatalog()
	msg := "mau beli abon sapi yang 500 gram, stok ready ga ?"
	intent := ResolveSalesIntent(msg, nil, true, true, profile, catalog)
	if intent.State != SalesStateCartReady {
		t.Fatalf("want cart_ready, got %+v", intent)
	}
}

func TestResolveCatalogMatch_stockFollowUpFromHistory(t *testing.T) {
	catalog := abonCatalog()
	history := []dbMessage{{
		Direction: "out",
		Body:      "Bisa kak, Abon Sapi 500G bisa beli per pcs.\nHarganya Rp35000/pcs.",
	}}
	match := resolveCatalogMatch("stoknya ready ?", history, catalog)
	if match == nil || match.Name != "Abon Sapi 500G" {
		t.Fatalf("want Abon from history, got %v", match)
	}
}

func TestResolveCatalogMatch_consultingQtyUsesFocusedHistory(t *testing.T) {
	catalog := mixedCatalog()
	history := []dbMessage{
		{Direction: "out", Body: "Produk:\nAbon Sapi 500G\n\nHarga:\nRp35000/pcs\n\nStok tersedia:\n• Gudang Utama: 20"},
		{Direction: "out", Body: "Maaf kak, produknya belum ketemu di katalog.\n\n🔥 Produk Pilihan\n• [3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SP...\nRp56900/paket"},
	}
	match := resolveCatalogMatch("mau beli 30 pcs bisa ?", history, catalog)
	if match == nil || !strings.Contains(match.Name, "Abon") {
		t.Fatalf("want Abon from focused history, got %v", match)
	}
}

func TestReplyFromBusinessCatalog_consultingQtyAbonNotBoxer(t *testing.T) {
	profile := &dbBusinessProfile{BusinessName: "Omah Apparel", Tone: strPtr("casual")}
	catalog := mixedCatalog()
	history := []dbMessage{{
		Direction: "out",
		Body:      "Produk:\nAbon Sapi 500G\n\nStok tersedia:\n• Gudang Utama: 20",
	}}
	reply, ok := replyFromBusinessCatalog("mau beli 30 pcs bisa ?", profile, catalog, history)
	if !ok {
		t.Fatal("expected retail policy reply")
	}
	if strings.Contains(reply, "eceran") || strings.Contains(reply, "paket isi 3") {
		t.Fatalf("should be Abon per-pcs policy, not boxer pack: %s", reply)
	}
	if !strings.Contains(reply, "Abon") && !strings.Contains(reply, "per pcs") {
		t.Fatalf("expected Abon retail reply: %s", reply)
	}
}

func TestAdvanceOrderFlow_stockGuard_qty5Reject_qty2Warehouse(t *testing.T) {
	catalog := []dbCatalogItem{{
		ID: "abon-1", Name: "Abon Sapi 500G", SellPrice: 35000, SellUnit: "pcs",
		StockTracked: true, StockAvailable: 30,
		StockByWarehouse: stockLines(2, 3),
	}}
	state := &orderState{
		Step: "ask_qty", CatalogItemID: "abon-1", ProductName: "Abon Sapi 500G",
		UnitPrice: 35000, SellUnit: "pcs",
	}
	profile := &dbBusinessProfile{BusinessName: "Toko", Tone: strPtr("casual")}

	reject := AdvanceOrderFlow(OrderFlowInput{
		UserText: "5 pcs", State: state, Catalog: catalog, Profile: profile,
	}, nil)
	if reject.Completed || reject.State == nil || reject.State.Step != "ask_qty" {
		t.Fatalf("qty 5 should stay on ask_qty, got %+v", reject)
	}
	if !strings.Contains(reject.Reply, "per gudang") {
		t.Fatalf("expected stock breakdown: %s", reject.Reply)
	}

	allow := AdvanceOrderFlow(OrderFlowInput{
		UserText: "2 pcs", State: state, Catalog: catalog, Profile: profile,
	}, nil)
	if allow.State == nil || allow.State.Step != "ask_recipient" {
		t.Fatalf("qty 2 should advance to ask_recipient, got %+v", allow)
	}
	if allow.State.WarehouseID != "wh-default" {
		t.Fatalf("warehouse = %q, want wh-default", allow.State.WarehouseID)
	}
}

func TestTranscriptContinuity_newOrderThenAbon(t *testing.T) {
	catalog := abonCatalog()
	profile := &dbBusinessProfile{BusinessName: "Omah Apparel"}

	if intent := ResolveSalesIntent("saya mau buat pesanan baru ya min", nil, false, true, profile, catalog); intent.State != SalesStateCartReady {
		t.Fatalf("step1 want cart_ready, got %s", intent.State)
	}
	if ShouldBreakOrderFlow("mau beli abon sapi yang 500 gram, stok ready ga ?", "ask_product", catalog) {
		t.Fatal("step2 should not break order flow")
	}
	if intent := ResolveSalesIntent("mau beli abon sapi yang 500 gram, stok ready ga ?", nil, true, true, profile, catalog); intent.State != SalesStateCartReady {
		t.Fatalf("step2 want cart_ready, got %s", intent.State)
	}
}
