package buyerflow

import (
	"strings"
	"testing"
)

// Fixture: t_omah_apparel draft WB-239488D0 (Cadbury + Durian + Maggi Berempah).
func omahWB239488Catalog() []CatalogItem {
	return append(omahCadburyLiveCatalog(), CatalogItem{
		ID: "durian-biscuit", ExternalCode: "Musang-king-durian-biscuit-240GRAM",
		Name: "Musang king durian biscuit 240G", SellPrice: 155000, SellUnit: "pcs",
	})
}

func omahWB239488Draft() *OrderState {
	st := &OrderState{
		Step:             "ask_recipient",
		PersistedOrderID: "239488d0-f98e-4ac7-b4b8-8f18f2ad42f6",
		Items: []OrderLineState{
			{CatalogItemID: "cad-bar", ProductName: "Cadbury biscoff bar 130 gram", Qty: 1, UnitPrice: 105000, SellUnit: "pcs"},
			{CatalogItemID: "durian-biscuit", ProductName: "Musang king durian biscuit 240G", Qty: 1, UnitPrice: 155000, SellUnit: "pcs"},
			{CatalogItemID: "maggi-berempah", ProductName: "Maggi Bumbu Ayam Goreng - Ayam Berempah", Qty: 1, UnitPrice: 70000, SellUnit: "pcs"},
		},
	}
	syncOrderStateFromItems(st)
	return st
}

func newWB239488Sim() *Simulator {
	sim := newOmahCadburySimulator()
	sim.Catalog = omahWB239488Catalog()
	sim.Order = omahWB239488Draft()
	return sim
}

func TestPadaPesananIsNotAdaPesananStatus(t *testing.T) {
	msg := "pada pesanan WB-239488D0 saya ga jadi beli maggi nya bisa ?"
	if IsOrderStatusInquiry(msg) {
		t.Fatal("pada pesanan must not match ada pesanan / status inquiry")
	}
	if IsOrderRefStatusLookup(msg) {
		t.Fatal("WB-ref + ga jadi maggi is line mutate, not status")
	}
	if !IsCartLineCorrectionIntent(msg) {
		t.Fatal("ga jadi beli maggi on a draft is a line correction")
	}
	aktif := "saya masih ada pesanan aktif ga ya ?"
	if !IsOrderStatusInquiry(aktif) && !IsActiveCheckoutRecapQuestion(aktif) {
		t.Fatal("real ada pesanan aktif must still match")
	}
}

func TestGaJadiBeliMaggiRemovesMaggiKeepsDraft(t *testing.T) {
	msg := "pada pesanan WB-239488D0 saya ga jadi beli maggi nya bisa ?"
	if IsOrderCancelRequest(msg) && !IsCartLineCorrectionIntent(msg) {
		t.Fatal("must not full-cancel the draft")
	}
	sim := newWB239488Sim()
	out := sim.Turn(msg)
	if out.Canceled {
		t.Fatalf("must not void draft, path=%s reply=%q", out.Path, out.Reply)
	}
	if out.Path == PathOrderStatus || out.Path == PathOrderCancel {
		t.Fatalf("path=%s want order_flow mutate, reply=%q", out.Path, out.Reply)
	}
	if out.Order == nil {
		t.Fatal("hydrated draft must stay")
	}
	if cartHasSKU(out.Order, "maggi-berempah") {
		t.Fatalf("Maggi Berempah must be excluded, cart=%+v", out.Order.Items)
	}
	if !cartHasSKU(out.Order, "cad-bar") || !cartHasSKU(out.Order, "durian-biscuit") {
		t.Fatalf("Cadbury + Durian must stay, cart=%+v", out.Order.Items)
	}
	if qtyOf(out.Order, "durian-biscuit") != 1 {
		t.Fatalf("durian qty must stay 1, cart=%+v", out.Order.Items)
	}
}

func TestNambahTandooriDurianMergesIntoDraft(t *testing.T) {
	msg := "pada pesanan WB-239488D0\n\nmau nambah Maggi Bumbu Ayam Goreng - Tandoori sama durian nya 1 lagi dong"
	if IsOrderStatusInquiry(msg) {
		t.Fatal("nambah on WB-ref is amend, not status")
	}
	sim := newWB239488Sim()
	out := sim.Turn(msg)
	if out.Canceled || out.Order == nil {
		t.Fatalf("must keep draft, canceled=%v path=%s reply=%q", out.Canceled, out.Path, out.Reply)
	}
	if !cartHasSKU(out.Order, "cad-bar") {
		t.Fatalf("Cadbury must merge-stay, cart=%+v", out.Order.Items)
	}
	if !cartHasSKU(out.Order, "maggi-tandoori") {
		t.Fatalf("Tandoori must be included, cart=%+v reply=%q", out.Order.Items, out.Reply)
	}
	if qtyOf(out.Order, "durian-biscuit") != 2 {
		t.Fatalf("durian qty want 2, cart=%+v reply=%q", out.Order.Items, out.Reply)
	}
	if !cartHasSKU(out.Order, "maggi-berempah") {
		t.Fatalf("existing Maggi Berempah must stay, cart=%+v", out.Order.Items)
	}
}

func TestCadburryCancelIsLineNotMissingOrder(t *testing.T) {
	msg := "cadburry nya saya mau cancel"
	if !IsCartLineCorrectionIntent(msg) {
		t.Fatal("cadburry cancel with a cart hit is line edit")
	}
	if IsExplicitPersistedOrderCancel(msg) {
		t.Fatal("SKU cancel must not be explicit full-order cancel")
	}
	sim := newWB239488Sim()
	out := sim.Turn(msg)
	if out.Canceled || out.Order == nil {
		t.Fatalf("must not void, path=%s reply=%q", out.Path, out.Reply)
	}
	if cartHasSKU(out.Order, "cad-bar") {
		t.Fatalf("Cadbury bar must be excluded, cart=%+v", out.Order.Items)
	}
	if !cartHasSKU(out.Order, "durian-biscuit") || !cartHasSKU(out.Order, "maggi-berempah") {
		t.Fatalf("other lines stay, cart=%+v", out.Order.Items)
	}
}

func TestLanjutkanBatalkanCadburyNambahDurianKeepsDraft(t *testing.T) {
	msg := "lanjutkan WB-239488D0, saya batalkan cadburry dan nambah durian musangking nya 1"
	if IsOrderCancelRequest(msg) && !IsCartLineCorrectionIntent(msg) {
		t.Fatal("mixed batalkan SKU + nambah is not full cancel")
	}
	if !IsCartLineCorrectionIntent(msg) {
		t.Fatal("mixed line cancel + add is cart correction")
	}
	sim := newWB239488Sim()
	out := sim.Turn(msg)
	if out.Canceled || out.Order == nil {
		t.Fatalf("status must stay draft, path=%s reply=%q", out.Path, out.Reply)
	}
	if stringsContainsCancelAll(out.Reply) {
		t.Fatalf("reply must not claim whole-order cancel: %q", out.Reply)
	}
	if cartHasSKU(out.Order, "cad-bar") {
		t.Fatalf("Cadbury excluded, cart=%+v", out.Order.Items)
	}
	if qtyOf(out.Order, "durian-biscuit") != 2 {
		t.Fatalf("durian qty want 2, cart=%+v reply=%q", out.Order.Items, out.Reply)
	}
	if !cartHasSKU(out.Order, "maggi-berempah") {
		t.Fatalf("Maggi Berempah stay, cart=%+v", out.Order.Items)
	}
}

func qtyOf(st *OrderState, id string) int {
	if st == nil {
		return 0
	}
	for _, ln := range st.Items {
		if ln.CatalogItemID == id {
			return ln.Qty
		}
	}
	if st.CatalogItemID == id {
		return st.Qty
	}
	return 0
}

func stringsContainsCancelAll(reply string) bool {
	low := strings.ToLower(reply)
	return strings.Contains(low, "ordernya dibatalkan") || strings.Contains(low, "pesanan dibatalkan")
}
