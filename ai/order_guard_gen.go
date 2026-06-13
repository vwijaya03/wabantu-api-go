package ai

import (
	"fmt"
	"strings"
)

// orderGuardCase — guard routing: order baru vs status, cancel clarification, multi-order ref.
type orderGuardCase struct {
	ID       int
	Category string
	Name     string
	Run      func(sim *ConversationSimulator) []TurnOutcome
	Assert   func(c orderGuardCase, outcomes []TurnOutcome) error
	Pure     func() error
}

func allOrderGuardCases() []orderGuardCase {
	var out []orderGuardCase
	id := 1
	add := func(cat, name string, run func(*ConversationSimulator) []TurnOutcome, assert func(orderGuardCase, []TurnOutcome) error) {
		out = append(out, orderGuardCase{ID: id, Category: cat, Name: name, Run: run, Assert: assert})
		id++
	}
	addPure := func(cat, name string, fn func() error) {
		out = append(out, orderGuardCase{ID: id, Category: cat, Name: name, Pure: fn})
		id++
	}

	// ── 1. Order baru ≠ status inquiry (12) ──
	newPurchaseMsgs := []string{
		"mau order boxer mono spot 10 paket bisa ?",
		"mau order boxer mono spot 10 paket bisa?",
		"bisa order boxer mono spot 5 paket?",
		"loh saya mau order barang woi",
		"mau pesan abon 3 biji boleh?",
		"kalau order de wasa 2 paket bisa ga?",
		"mau beli boxer pria mono spot 1 paket bisa?",
		"boleh order hello kitty 2 pcs?",
		"mau order barang dong",
		"apakah bisa order boxer mono spot 10 paket",
		"mau pesen boxer mono spot 5 paket ya",
		"order baru boxer mono spot 3 paket",
	}
	for _, msg := range newPurchaseMsgs {
		msg := msg
		addPure("new_purchase_not_status", truncGuardName(msg), func() error {
			if IsOrderStatusInquiry(msg) {
				return fmt.Errorf("IsOrderStatusInquiry(%q) want false", msg)
			}
			if !IsNewPurchaseIntentQuestion(msg) {
				return fmt.Errorf("IsNewPurchaseIntentQuestion(%q) want true", msg)
			}
			return nil
		})
	}

	// ── 2. Cancel clarification ≠ cancel command (10) ──
	clarifyMsgs := []string{
		"order mana yang kamu batalkan ?",
		"pesanan mana yang dibatalkan?",
		"kok order saya dibatalkan?",
		"kenapa dibatalkan?",
		"order apa yang barusan dibatalin?",
		"yang kamu batalkan order yang mana?",
		"order mana yang lu batalkan?",
		"kok dibatalkan pesanan saya?",
		"order yang dibatalkan yang mana?",
	}
	addPure("cancel_clarification", "pesanan_mana_tadi", func() error {
		msg := "pesanan mana tadi?"
		if !IsOrderStatusInquiry(msg) {
			return fmt.Errorf("IsOrderStatusInquiry(%q) want true", msg)
		}
		if IsDraftOrderCancelRequest(msg) {
			return fmt.Errorf("IsDraftOrderCancelRequest(%q) want false", msg)
		}
		return nil
	})
	for _, msg := range clarifyMsgs {
		msg := msg
		addPure("cancel_clarification", truncGuardName(msg), func() error {
			if !IsCancelClarificationQuestion(msg) {
				return fmt.Errorf("IsCancelClarificationQuestion(%q) want true", msg)
			}
			if IsDraftOrderCancelRequest(msg) {
				return fmt.Errorf("IsDraftOrderCancelRequest(%q) want false", msg)
			}
			if !IsOrderStatusInquiry(msg) {
				return fmt.Errorf("IsOrderStatusInquiry(%q) want true", msg)
			}
			return nil
		})
	}

	// ── 3. Reset percakapan lalu order baru (8) ──
	resetScripts := []struct {
		name string
		msgs []string
	}{
		{"halo_mono", []string{"halo", "mau order boxer mono spot 10 paket bisa ?"}},
		{"halo_pesan", []string{"halo kak", "mau pesan boxer mono spot 5 paket"}},
		{"halo_abon", []string{"halo", "order abon 3 biji"}},
		{"halo_barang", []string{"selamat pagi", "loh saya mau order barang woi"}},
		{"batal_halo_order", []string{"mau pesen boxer mono spot 2 paket", "batal", "halo", "mau order boxer mono spot 10 paket"}},
		{"cancel_halo_order", []string{"order abon 2 biji", "cancel", "halo", "mau 3 biji abon"}},
		{"greeting_then_de_wasa", []string{"halo", "mau de wasa 3 paket"}},
		{"greeting_then_hello_kitty", []string{"hai kak", "hello kitty anak 2 pcs"}},
	}
	for _, sc := range resetScripts {
		sc := sc
		add("reset_then_order", sc.name, func(sim *ConversationSimulator) []TurnOutcome {
			return sim.RunScript(sc.msgs...)
		}, func(_ orderGuardCase, o []TurnOutcome) error {
			last := o[len(o)-1]
			if last.Path == PathOrderStatus {
				return fmt.Errorf("after reset, last path should not be order_status, got %q for %v", last.Path, sc.msgs)
			}
			if strings.Contains(sc.msgs[len(sc.msgs)-1], "barang woi") {
				if last.Path != PathConsulting && last.Path != PathCatalogDB && last.Order == nil {
					return fmt.Errorf("generic order barang should route to catalog/consulting, got %s", last.Path)
				}
				return nil
			}
			if last.Order == nil && last.Path != PathCatalogDB {
				return fmt.Errorf("expected order or catalog after restart: %+v", last)
			}
			if last.Order != nil && strings.Contains(sc.msgs[len(sc.msgs)-1], "mono spot") &&
				strings.Contains(strings.ToLower(last.Order.ProductName), "hello kitty") {
				return fmt.Errorf("should not pick hello kitty after mono spot order")
			}
			return nil
		})
	}

	// ── 4. Status inquiry sah (10) ──
	statusMsgs := []string{
		"pesanan yang atas nama saya ada kah ?",
		"status pesanan saya",
		"nomor pesanan saya apa?",
		"cek pesanan saya dong",
		"ada pesanan aktif ga?",
		"order saya gimana?",
		"lihat pesanan saya",
		"punya pesanan ga dari chat ini?",
		"pesanan ku udah diproses belum?",
		"orderan saya mana?",
	}
	for _, msg := range statusMsgs {
		msg := msg
		addPure("status_inquiry_true", truncGuardName(msg), func() error {
			if !IsOrderStatusInquiry(msg) {
				return fmt.Errorf("IsOrderStatusInquiry(%q) want true", msg)
			}
			if IsNewPurchaseIntentQuestion(msg) {
				return fmt.Errorf("IsNewPurchaseIntentQuestion(%q) want false", msg)
			}
			return nil
		})
	}

	// ── 5. Parse nomor pesanan WB- (5) ──
	refCases := []struct{ msg, want string }{
		{"batalkan WB-EAA94534", "WB-EAA94534"},
		{"status wb-eaa94534", "WB-EAA94534"},
		{"cek pesanan WB-EB76635C dong", "WB-EB76635C"},
		{"halo kak", ""},
		{"order mono spot", ""},
	}
	for _, rc := range refCases {
		rc := rc
		addPure("order_ref_parse", truncGuardName(rc.msg), func() error {
			got := parseOrderRefFromMessage(rc.msg)
			if got != rc.want {
				return fmt.Errorf("parseOrderRefFromMessage(%q) = %q want %q", rc.msg, got, rc.want)
			}
			return nil
		})
	}

	// ── 6. Replay thread produksi Jun 2026 (5) ──
	addPure("production_thread", "mono_spot_not_status", func() error {
		msg := "mau order boxer mono spot 10 paket bisa ?"
		if IsOrderStatusInquiry(msg) {
			return fmt.Errorf("production bug: new order misrouted as status")
		}
		return nil
	})
	addPure("production_thread", "clarify_not_cancel", func() error {
		msg := "order mana yang kamu batalkan ?"
		if IsDraftOrderCancelRequest(msg) {
			return fmt.Errorf("production bug: clarification treated as cancel")
		}
		if !IsOrderStatusInquiry(msg) {
			return fmt.Errorf("clarification should be status inquiry")
		}
		return nil
	})
	add("production_thread", "halo_then_mono", func(sim *ConversationSimulator) []TurnOutcome {
		return sim.RunScript("halo", "mau order boxer mono spot 10 paket bisa ?")
	}, func(_ orderGuardCase, o []TurnOutcome) error {
		if o[1].Path == PathOrderStatus {
			return fmt.Errorf("halo→order should not hit order_status")
		}
		return nil
	})
	add("production_thread", "complaint_then_order", func(sim *ConversationSimulator) []TurnOutcome {
		return sim.RunScript("order mana yang kamu batalkan ?", "mau order boxer mono spot 10 paket bisa ?")
	}, func(_ orderGuardCase, o []TurnOutcome) error {
		if o[0].Path != PathOrderStatus {
			return fmt.Errorf("clarification turn want order_status got %s", o[0].Path)
		}
		if o[1].Path == PathOrderStatus {
			return fmt.Errorf("new order after clarification should not be order_status")
		}
		return nil
	})
	addPure("production_thread", "explicit_cancel_still_works", func() error {
		for _, msg := range []string{"batalkan pesanan", "batal", "cancel"} {
			if !ShouldCancelPersistedOrder(msg) && msg != "batal" && msg != "cancel" {
				continue
			}
			if !IsDraftOrderCancelRequest(msg) {
				return fmt.Errorf("explicit cancel %q broken", msg)
			}
		}
		return nil
	})

	if len(out) != 50 {
		panic(fmt.Sprintf("order guard cases: want 50 got %d", len(out)))
	}
	return out
}

func truncGuardName(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, " ", "_"), "?", "")
	if len(s) > 40 {
		return s[:40]
	}
	return s
}
