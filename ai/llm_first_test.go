package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	bf "encore.app/wabantu/internal/buyerflow"
	"encore.app/wabantu/internal/salesbrain"
	"encore.app/wabantu/order"
)

// ── time helpers ─────────────────────────────────────────────────────────────

func TestLLMFirstGreetingForWIBBoundaries(t *testing.T) {
	utc := func(h, m int) time.Time { return time.Date(2026, 10, 17, h, m, 0, 0, time.UTC) }
	cases := []struct {
		at   time.Time
		want string
	}{
		{utc(21, 59), "malam"}, // 04:59 WIB
		{utc(22, 0), "pagi"},   // 05:00 WIB
		{utc(3, 59), "pagi"},   // 10:59 WIB
		{utc(4, 0), "siang"},   // 11:00 WIB
		{utc(7, 59), "siang"},  // 14:59 WIB
		{utc(8, 0), "sore"},    // 15:00 WIB
		{utc(11, 59), "sore"},  // 18:59 WIB
		{utc(12, 0), "malam"},  // 19:00 WIB
		{utc(17, 0), "malam"},  // 00:00 WIB
	}
	for _, c := range cases {
		if got := greetingFor(c.at); got != c.want {
			t.Errorf("greetingFor(%s) = %q, want %q", c.at.Format(time.RFC3339), got, c.want)
		}
	}
}

func TestLLMFirstFormatNowID(t *testing.T) {
	cases := []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, 10, 17, 7, 20, 0, 0, time.UTC), "Sabtu 17 Okt 2026 14:20 WIB"},
		// 20:05 UTC on the 31st is already the next day in Jakarta.
		{time.Date(2026, 12, 31, 20, 5, 0, 0, time.UTC), "Jumat 1 Jan 2027 03:05 WIB"},
		{time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), "Minggu 2 Agu 2026 07:00 WIB"},
	}
	for _, c := range cases {
		if got := formatNowID(c.at); got != c.want {
			t.Errorf("formatNowID(%s) = %q, want %q", c.at.Format(time.RFC3339), got, c.want)
		}
	}
}

// ── history ──────────────────────────────────────────────────────────────────

func lfMsg(author, body string, at time.Time) dbMessage {
	return dbMessage{ID: author + at.Format("150405"), Direction: map[bool]string{true: "in", false: "out"}[author == "contact"], Author: author, Type: "text", Body: body, CreatedAt: at}
}

func TestLLMFirstHistoryTurnsFrom(t *testing.T) {
	base := time.Date(2026, 10, 17, 7, 0, 0, 0, time.UTC)
	history := []dbMessage{
		lfMsg("contact", "halo kak", base),
		lfMsg("ai", "Halo! Mau cari apa?", base.Add(time.Minute)),
		lfMsg("system", "Maaf Kak, sistem kami sedang ada gangguan.", base.Add(2*time.Minute)),
		lfMsg("human", "Saya admin, ada yang bisa dibantu?", base.Add(3*time.Minute)),
		{ID: "img", Direction: "in", Author: "contact", Type: "image", Body: "", CreatedAt: base.Add(4 * time.Minute)},
		lfMsg("contact", "is it frozen?", base.Add(5*time.Minute)), // the current inbound
	}
	got := historyTurnsFrom(history, "is it frozen?")
	want := []salesbrain.HistoryTurn{
		{Role: salesbrain.RoleBuyer, Text: "halo kak"},
		{Role: salesbrain.RoleShop, Text: "Halo! Mau cari apa?"},
		{Role: salesbrain.RoleShop, Text: "Maaf Kak, sistem kami sedang ada gangguan."},
		{Role: salesbrain.RoleStaff, Text: "Saya admin, ada yang bisa dibantu?"},
		{Role: salesbrain.RoleBuyer, Text: "[image]"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d turns %+v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("turn %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestLLMFirstHistoryTurnsDropsCoalescedBurst(t *testing.T) {
	base := time.Date(2026, 10, 17, 7, 0, 0, 0, time.UTC)
	history := []dbMessage{
		lfMsg("ai", "Mau pesan apa Kak?", base),
		lfMsg("contact", "mau salmon", base.Add(time.Minute)),
		lfMsg("contact", "100 gram 2 pcs", base.Add(time.Minute+5*time.Second)),
	}
	got := historyTurnsFrom(history, "mau salmon\n100 gram 2 pcs")
	if len(got) != 1 || got[0].Role != salesbrain.RoleShop {
		t.Fatalf("the whole burst is the current message and must be dropped, got %+v", got)
	}
	// A text that is not the trailing run leaves the history alone.
	if got := historyTurnsFrom(history, "something else"); len(got) != 3 {
		t.Fatalf("history of a different message must be kept whole, got %d turns", len(got))
	}
}

func TestLLMFirstPrevBuyerMessageAtAndBuyerTexts(t *testing.T) {
	base := time.Date(2026, 10, 17, 7, 0, 0, 0, time.UTC)
	history := []dbMessage{
		lfMsg("contact", "first", base),
		lfMsg("ai", "reply", base.Add(time.Minute)),
		lfMsg("contact", "second", base.Add(2*time.Minute)),
		lfMsg("ai", "reply 2", base.Add(3*time.Minute)),
		lfMsg("contact", "third", base.Add(4*time.Minute)),
		lfMsg("contact", "fourth", base.Add(5*time.Minute)),
		lfMsg("contact", "now", base.Add(6*time.Minute)), // current
	}
	if got := lfPrevBuyerMessageAt(history, "now"); !got.Equal(base.Add(5 * time.Minute)) {
		t.Fatalf("previous buyer message at %v, want %v", got, base.Add(5*time.Minute))
	}
	if got := lfPrevBuyerMessageAt([]dbMessage{lfMsg("contact", "now", base)}, "now"); !got.IsZero() {
		t.Fatalf("first message ever has no previous one, got %v", got)
	}
	texts := lfBuyerTexts(history, "now")
	wantTexts := []string{"now", "fourth", "third", "second"}
	if len(texts) != len(wantTexts) {
		t.Fatalf("buyer texts %v, want current + 3 previous %v", texts, wantTexts)
	}
	for i := range wantTexts {
		if texts[i] != wantTexts[i] {
			t.Fatalf("buyer texts %v, want %v", texts, wantTexts)
		}
	}
}

// ── open orders ──────────────────────────────────────────────────────────────

func lfTestOrder(t *testing.T, id, status, payment string, created time.Time, items []order.OrderItem, ship order.ShippingAddress) persistedOrder {
	t.Helper()
	itemsJSON, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	shipJSON, err := json.Marshal(ship)
	if err != nil {
		t.Fatal(err)
	}
	var subtotal float64
	for _, it := range items {
		subtotal += it.Qty * it.UnitPrice
	}
	return persistedOrder{ID: id, Status: status, PaymentStatus: payment, ItemsJSON: itemsJSON, ShippingJSON: shipJSON, Subtotal: subtotal, Total: subtotal, CreatedAt: created}
}

var (
	lfTunaItem   = order.OrderItem{CatalogItemID: "tuna-100", Name: "Yellow Tail Tuna Portion 100 Gram", Qty: 1, UnitPrice: 350000}
	lfSalmonItem = order.OrderItem{CatalogItemID: "salmon-100", Name: "Salmon Steak 100 Gram", Qty: 2, UnitPrice: 75000}
	lfFullShip   = order.ShippingAddress{Name: "Ana", Phone: "+62872839472828", Street: "Villa Kalijudan Indah A9", City: "Surabaya", Province: "Jawa Timur", PostalCode: "60177"}
)

func TestLLMFirstMapOpenOrders(t *testing.T) {
	now := time.Date(2026, 10, 17, 7, 0, 0, 0, time.UTC)
	ago := func(h int) time.Time { return now.Add(-time.Duration(h) * time.Hour) }
	orders := []persistedOrder{
		lfTestOrder(t, "f4bfce06-0000-0000-0000-000000000001", "draft", "unpaid", ago(1), []order.OrderItem{lfTunaItem}, order.ShippingAddress{Country: "Indonesia"}),
		lfTestOrder(t, "aaaaaaaa-0000-0000-0000-000000000002", "draft", "unpaid", ago(5), []order.OrderItem{lfTunaItem, lfSalmonItem}, order.ShippingAddress{Name: "Ana", Phone: "+62872839472828"}),
		lfTestOrder(t, "bbbbbbbb-0000-0000-0000-000000000003", "draft", "unpaid", ago(8), []order.OrderItem{lfSalmonItem}, lfFullShip),
		lfTestOrder(t, "cccccccc-0000-0000-0000-000000000004", "processing", "verified", ago(2), []order.OrderItem{lfTunaItem}, lfFullShip),
		lfTestOrder(t, "dddddddd-0000-0000-0000-000000000005", "shipped", "verified", ago(3), []order.OrderItem{lfTunaItem}, lfFullShip),
		lfTestOrder(t, "eeeeeeee-0000-0000-0000-000000000006", "paid", "verified", ago(3), []order.OrderItem{lfTunaItem}, lfFullShip),
		lfTestOrder(t, "ffffffff-0000-0000-0000-000000000007", "draft", "proof_submitted", ago(3), []order.OrderItem{lfTunaItem}, lfFullShip),
		lfTestOrder(t, "11111111-0000-0000-0000-000000000008", "draft", "unpaid", ago(100), []order.OrderItem{lfTunaItem}, lfFullShip), // older than 72h
		lfTestOrder(t, "22222222-0000-0000-0000-000000000009", "draft", "unpaid", ago(2), nil, order.ShippingAddress{}),                // no items
	}
	got := mapOpenOrders(orders, now)

	if len(got) != 3 {
		t.Fatalf("want the 3 open orders (drafts and one awaiting payment), got %d: %+v", len(got), got)
	}
	// Newest first.
	if got[0].Ref != "WB-F4BFCE06" || got[1].Ref != "WB-AAAAAAAA" || got[2].Ref != "WB-BBBBBBBB" {
		t.Fatalf("order/refs wrong: %s %s %s", got[0].Ref, got[1].Ref, got[2].Ref)
	}
	if got[0].Status != salesbrain.StatusDraft || got[0].Stage != salesbrain.StageRecipient {
		t.Errorf("draft without recipient: status %q stage %q", got[0].Status, got[0].Stage)
	}
	if got[1].Status != salesbrain.StatusDraft || got[1].Stage != salesbrain.StageAddress {
		t.Errorf("draft with recipient only: status %q stage %q", got[1].Status, got[1].Stage)
	}
	if got[2].Status != salesbrain.StatusAwaitingPayment || got[2].Stage != salesbrain.StagePayment {
		t.Errorf("draft with a complete address is waiting for payment: status %q stage %q", got[2].Status, got[2].Stage)
	}
	if len(got[1].Lines) != 2 || got[1].Lines[0].ID != "tuna-100" || got[1].Lines[1].Qty != 2 || got[1].Lines[1].UnitPrice != 75000 {
		t.Errorf("lines not mapped: %+v", got[1].Lines)
	}
	if got[1].Subtotal != 350000+150000 {
		t.Errorf("subtotal = %d, want 500000", got[1].Subtotal)
	}
}

func TestLLMFirstMapOpenOrdersKeepsAtMostThree(t *testing.T) {
	now := time.Date(2026, 10, 17, 7, 0, 0, 0, time.UTC)
	var orders []persistedOrder
	for i := 0; i < 5; i++ {
		orders = append(orders, lfTestOrder(t, strings.Repeat(string(rune('a'+i)), 8)+"-0000-0000-0000-000000000000", "draft", "unpaid",
			now.Add(-time.Duration(i+1)*time.Hour), []order.OrderItem{lfTunaItem}, order.ShippingAddress{}))
	}
	if got := mapOpenOrders(orders, now); len(got) != 3 {
		t.Fatalf("at most three open orders are listed, got %d", len(got))
	}
}

// ── catalog, KB and profile facts ────────────────────────────────────────────

func TestLLMFirstCatalogFacts(t *testing.T) {
	catalog := []dbCatalogItem{
		{ID: "beef-500", ExternalCode: "BEEF500", Name: "Australian Beef 500 Gram", SellPrice: 450000, SellUnit: "pcs", Description: "Sold frozen and vacuum-sealed", StockTracked: true, StockAvailable: 4},
		{ID: "salmon-100", ExternalCode: "SAL100", Name: "Salmon Steak 100 Gram", SellPrice: 75000.4, SellUnit: "pcs", StockTracked: true, StockAvailable: 0},
		{ID: "tuna-100", Name: "Yellow Tail Tuna Portion 100 Gram", SellPrice: 350000, SellUnit: "pcs"},
	}
	got := lfCatalogFacts(catalog)
	if len(got) != 3 {
		t.Fatalf("got %d facts", len(got))
	}
	if got[0].No != 1 || got[1].No != 2 || got[2].No != 3 {
		t.Errorf("rows are numbered 1-based in load order: %d %d %d", got[0].No, got[1].No, got[2].No)
	}
	if got[0].ID != "beef-500" || got[0].Code != "BEEF500" || got[0].Price != 450000 || got[0].Unit != "pcs" || got[0].Description != "Sold frozen and vacuum-sealed" {
		t.Errorf("fact 1 = %+v", got[0])
	}
	if got[0].Stock != "tersedia" || got[1].Stock != "habis" || got[2].Stock != "" {
		t.Errorf("stock is shown only for tracked items: %q %q %q", got[0].Stock, got[1].Stock, got[2].Stock)
	}
	if got[1].Price != 75000 {
		t.Errorf("price rounds to whole rupiah, got %d", got[1].Price)
	}
}

func TestLLMFirstKBAndProfileFacts(t *testing.T) {
	cat := "pembayaran"
	kb := lfKBFacts([]dbKBEntry{{Question: "Metode bayar?", Answer: "BCA - 000111222 - Omah Apparel", Category: &cat}, {Question: "Jam kirim?", Answer: "08:00-12:00"}})
	if len(kb) != 2 || kb[0].Category != "pembayaran" || kb[1].Category != "" || kb[0].Answer != "BCA - 000111222 - Omah Apparel" {
		t.Fatalf("kb facts = %+v", kb)
	}
	desc, addr, hours, area, tone := "Frozen seafood", "Surabaya", "08:00-19:00", "Jawa", "formal"
	p := lfProfileFact(&dbBusinessProfile{BusinessName: "Omah Apparel", Description: &desc, Address: &addr, OpeningHours: &hours, DeliveryArea: &area, Tone: &tone})
	if p.Name != "Omah Apparel" || p.Description != desc || p.Address != addr || p.OpeningHours != hours || p.DeliveryArea != area || p.Tone != tone {
		t.Fatalf("profile fact = %+v", p)
	}
	if got := lfProfileFact(&dbBusinessProfile{BusinessName: "Toko"}); got.Description != "" || got.Tone != "" {
		t.Fatalf("nil pointers read as empty: %+v", got)
	}
}

func lfBigCatalog() []salesbrain.CatalogFact {
	var facts []salesbrain.CatalogFact
	for i := 1; i <= 30; i++ {
		facts = append(facts, salesbrain.CatalogFact{No: i, ID: "item-" + strings.Repeat("x", i%3) + string(rune('a'+i%26)), Name: "Produk Biasa Nomor " + string(rune('A'+i%26)), Price: int64(10000 * i)})
	}
	facts[4] = salesbrain.CatalogFact{No: 5, ID: "salmon-100", Name: "Salmon Steak 100 Gram", Price: 75000}
	facts[11] = salesbrain.CatalogFact{No: 12, ID: "salmon-250", Name: "Salmon Steak 250 Gram", Price: 105000}
	facts[19] = salesbrain.CatalogFact{No: 20, ID: "frozen-1", Name: "Daging Kemasan", Price: 99000, Description: "Sold frozen and vacuum-sealed"}
	facts[27] = salesbrain.CatalogFact{No: 28, ID: "tuna-100", Name: "Yellow Tail Tuna Portion 100 Gram", Price: 350000}
	return facts
}

func TestLLMFirstTopKCatalogKeepsBestMatchesAndCartItems(t *testing.T) {
	facts := lfBigCatalog()
	cart := []salesbrain.OrderLine{{ID: "tuna-100", Name: "Yellow Tail Tuna Portion 100 Gram", Qty: 1, UnitPrice: 350000}}

	got := topKCatalog(facts, "berapa harga salmon steak?", cart, 2)

	byNo := map[int]bool{}
	for i, f := range got {
		byNo[f.No] = true
		if i > 0 && got[i-1].No >= f.No {
			t.Fatalf("result must keep catalog order, got %d before %d", got[i-1].No, f.No)
		}
	}
	if !byNo[5] || !byNo[12] {
		t.Errorf("the two salmon rows must be kept, got %v", byNo)
	}
	if !byNo[28] {
		t.Errorf("every cart item must be kept even when it does not match the message, got %v", byNo)
	}
	if len(got) != 3 {
		t.Errorf("want k=2 matches + 1 cart item, got %d rows", len(got))
	}
	// Row numbers are the prompt's contract with ApplyItemOps: they are never renumbered.
	for _, f := range got {
		if f.Name == "Salmon Steak 100 Gram" && f.No != 5 {
			t.Errorf("row number changed: %+v", f)
		}
	}
}

func TestLLMFirstTopKCatalogUsesDescriptionAndSmallCatalogs(t *testing.T) {
	facts := lfBigCatalog()
	got := topKCatalog(facts, "is it frozen and vacuum sealed", nil, 1)
	if len(got) != 1 || got[0].No != 20 {
		t.Fatalf("the description must count for matching, got %+v", got)
	}
	if all := topKCatalog(facts[:3], "anything", nil, 12); len(all) != 3 {
		t.Fatalf("a catalog smaller than k is returned whole, got %d", len(all))
	}
	// Nothing matches: the first k rows (stable) stand in, never an empty catalog.
	if got := topKCatalog(facts, "zzzz qqqq", nil, 4); len(got) != 4 || got[0].No != 1 {
		t.Fatalf("no match keeps the first rows, got %+v", got)
	}
}

// ── model tokens, reminders, replies ─────────────────────────────────────────

func TestLLMFirstRemindStateRoundTrip(t *testing.T) {
	at := time.Date(2026, 10, 17, 7, 0, 0, 0, time.UTC)
	rs := lfNextRemindState(salesbrain.ReminderState{}, "WB-F4BFCE06", at)
	rs = lfNextRemindState(rs, "WB-F4BFCE06", at.Add(time.Hour))
	rs = lfNextRemindState(rs, "WB-AAAAAAAA", at.Add(2*time.Hour))
	if rs.Count["WB-F4BFCE06"] != 2 || rs.Count["WB-AAAAAAAA"] != 1 || !rs.LastAt.Equal(at.Add(2*time.Hour)) {
		t.Fatalf("next state wrong: %+v", rs)
	}

	raw := lfEncodeRemind(rs)
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	if _, ok := generic["lastAt"]; !ok {
		t.Errorf("stored JSON must use the lastAt key: %s", raw)
	}
	if _, ok := generic["count"]; !ok {
		t.Errorf("stored JSON must use the count key: %s", raw)
	}
	back := lfDecodeRemind(raw)
	if !back.LastAt.Equal(rs.LastAt) || back.Count["WB-F4BFCE06"] != 2 {
		t.Fatalf("round trip lost data: %+v", back)
	}
	if got := lfDecodeRemind([]byte("not json")); !got.LastAt.IsZero() || len(got.Count) != 0 {
		t.Fatalf("garbage must read as no state, got %+v", got)
	}
	if got := lfDecodeRemind(nil); !got.LastAt.IsZero() {
		t.Fatalf("empty must read as no state, got %+v", got)
	}
}

func TestLLMFirstReplyMentions(t *testing.T) {
	if !lfReplyMentionsRef("Kakak masih punya pesanan wb-f4bfce06 yang belum selesai", "WB-F4BFCE06") {
		t.Error("the ref match ignores case")
	}
	if lfReplyMentionsRef("halo", "WB-F4BFCE06") || lfReplyMentionsRef("halo WB-F4BFCE06", "") {
		t.Error("no ref, no mention")
	}

	facts := []salesbrain.CatalogFact{
		{No: 1, ID: "tuna-100", Name: "Yellow Tail Tuna Portion 100 Gram"},
		{No: 2, ID: "salmon-100", Name: "Salmon Steak 100 Gram"},
	}
	cart := []salesbrain.OrderLine{{ID: "tuna-100", Name: "Yellow Tail Tuna Portion 100 Gram", Qty: 1, UnitPrice: 350000}}
	if lfReplyOffersUpsell("Siap, 1x Yellow Tail Tuna Portion 100 Gram sudah masuk.", facts, cart) {
		t.Error("naming a cart item is not an upsell")
	}
	if !lfReplyOffersUpsell("Mau sekalian tambah Salmon Steak 100 Gram, Kak?", facts, cart) {
		t.Error("offering a product that is not in the cart is an upsell")
	}
	if lfReplyOffersUpsell("Terima kasih Kak!", facts, cart) {
		t.Error("no product named, no upsell")
	}
}

func TestLLMFirstViolationCodes(t *testing.T) {
	if got := lfViolationCodes(nil); got != "" {
		t.Fatalf("got %q", got)
	}
	got := lfViolationCodes([]salesbrain.Violation{{Code: salesbrain.VAmount, Detail: "Rp300.000"}, {Code: salesbrain.VLanguage, Detail: "id"}})
	if got != "amount_not_in_facts,language_mismatch" || strings.Contains(got, "300") {
		t.Fatalf("only the codes are logged, got %q", got)
	}
}

func TestLLMFirstAddressLineAndProblems(t *testing.T) {
	st := orderState{Street: "Villa Kalijudan Indah A9", Kecamatan: "Mulyorejo", City: "Surabaya", Province: "Jawa Timur", PostalCode: "60177"}
	if got := lfAddressLine(&st); got != "Villa Kalijudan Indah A9, Mulyorejo, Surabaya, Jawa Timur, 60177" {
		t.Fatalf("address line = %q", got)
	}
	if got := lfAddressLine(&orderState{City: "Surabaya"}); got != "Surabaya" {
		t.Fatalf("empty parts are skipped, got %q", got)
	}
	if got := lfJoinProblems([]string{"a", "b"}); got != "a; b" {
		t.Fatalf("problems join with a semicolon (no language-specific connector), got %q", got)
	}
}

// ── language persistence ─────────────────────────────────────────────────────

// lfRecordingTS counts the writes of a turn; it answers the way fakeTS does otherwise.
type lfRecordingTS struct {
	fakeTS
	execs   []string
	execErr error
}

func (r *lfRecordingTS) ExecContext(_ context.Context, q string, _ ...any) (sql.Result, error) {
	r.execs = append(r.execs, q)
	if r.execErr != nil {
		return nil, r.execErr
	}
	return fakeResult{}, nil
}

// After salesbrain locks a language the in-memory contact and the turn's saved-language stack
// must follow: the legacy templates (when this turn fails open) and the deferred legacy
// applyBuyerLanguageFromMessage of ProcessAutoReply read exactly those two.
func TestLLMFirstPersistLanguageKeepsLegacyConsistent(t *testing.T) {
	defer bf.PushSavedBuyerLang("")()
	ctx := context.Background()
	ts := &lfRecordingTS{}
	contact := &dbContact{ID: "k1"}

	if err := lfPersistLanguage(ctx, ts, contact, salesbrain.LangEN); err != nil {
		t.Fatal(err)
	}
	if contactPreferredLanguage(contact) != "en" {
		t.Fatalf("contact.PreferredLanguage = %q, want en", contactPreferredLanguage(contact))
	}
	if got := bf.CurrentSavedBuyerLang(); got != "en" {
		t.Fatalf("saved buyer language = %q, want en", got)
	}
	if len(ts.execs) != 1 || !strings.Contains(ts.execs[0], "preferred_language") {
		t.Fatalf("exactly one preferred_language write expected, got %v", ts.execs)
	}

	// The deferred legacy hook sees the persisted language and leaves it alone.
	s := &AutoReplyService{}
	for _, msg := range []string{
		"Please reply in English",
		"selamat pagi kak, saya mau pesan salmon 2 pcs ya",
		"halo",
	} {
		s.applyBuyerLanguageFromMessage(ctx, ts, contact, msg, false)
		if contactPreferredLanguage(contact) != "en" {
			t.Fatalf("legacy hook overrode the locked language on %q: %q", msg, contactPreferredLanguage(contact))
		}
	}
	if len(ts.execs) != 1 {
		t.Fatalf("the legacy hook must not write again, got %d writes", len(ts.execs))
	}
}

func TestLLMFirstPersistLanguageFailureChangesNothing(t *testing.T) {
	defer bf.PushSavedBuyerLang("id")()
	ts := &lfRecordingTS{execErr: errors.New("db down")}
	contact := &dbContact{ID: "k1", PreferredLanguage: strPtr("id")}

	if err := lfPersistLanguage(context.Background(), ts, contact, salesbrain.LangEN); err == nil {
		t.Fatal("a failed write must be reported")
	}
	if contactPreferredLanguage(contact) != "id" || bf.CurrentSavedBuyerLang() != "id" {
		t.Fatalf("a failed write must not change the in-memory language: contact=%q stack=%q",
			contactPreferredLanguage(contact), bf.CurrentSavedBuyerLang())
	}
}
