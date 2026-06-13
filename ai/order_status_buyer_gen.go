package ai

import (
	"fmt"
	"strings"
)

type orderStatusBuyerCase struct {
	ID       int
	Category string
	Name     string
	Run      func(sim *ConversationSimulator) []TurnOutcome
	Assert   func(c orderStatusBuyerCase, outcomes []TurnOutcome) error
	Pure     func() error
}

func allOrderStatusBuyerCases() []orderStatusBuyerCase {
	var out []orderStatusBuyerCase
	id := 1
	add := func(cat, name string, run func(*ConversationSimulator) []TurnOutcome, assert func(orderStatusBuyerCase, []TurnOutcome) error) {
		out = append(out, orderStatusBuyerCase{ID: id, Category: cat, Name: name, Run: run, Assert: assert})
		id++
	}
	addPure := func(cat, name string, fn func() error) {
		out = append(out, orderStatusBuyerCase{ID: id, Category: cat, Name: name, Pure: fn})
		id++
	}

	// ── 1. Greeting + status inquiry (10) — thread produksi Jun 2026 ──
	greetingStatusMsgs := []string{
		"halo min apakah saya punya pesanan aktif ?",
		"halo min apakah saya punya pesanan pending ?",
		"apa saya masih punya pesanan yang pending ya ?",
		"saya punya pesanan nggak ?",
		"apakah saya punya pesanan ?",
		"min ada pesanan aktif ga?",
		"halo kak cek pesanan saya dong",
		"selamat pagi min punya order ga?",
		"hai min status pesanan saya gimana?",
	}
	for _, msg := range greetingStatusMsgs {
		msg := msg
		addPure("greeting_status", truncStatusName(msg), func() error {
			if !IsOrderStatusInquiry(msg) {
				return fmt.Errorf("IsOrderStatusInquiry(%q) want true", msg)
			}
			if IsGreetingLike(msg) {
				return fmt.Errorf("IsGreetingLike(%q) want false — commerce/status wins", msg)
			}
			return nil
		})
	}

	// ── 2. Active/pending only (8) ──
	activeMsgs := []string{
		"apa saya masih punya pesanan yang pending ya ?",
		"saya punya pesanan nggak ?",
		"ada pesanan aktif ga?",
		"punya order pending?",
		"masih ada pesanan aktif?",
		"pesanan pending saya ada?",
		"order aktif saya ada ga?",
		"masih punya pesanan ga?",
	}
	for _, msg := range activeMsgs {
		msg := msg
		addPure("active_only", truncStatusName(msg), func() error {
			if !WantsActiveOrderOnly(msg) {
				return fmt.Errorf("WantsActiveOrderOnly(%q) want true", msg)
			}
			return nil
		})
	}

	// ── 3. Cancel harus pilih nomor (6) ──
	addPure("cancel_pick_ref", "no_ref_not_explicit_match", func() error {
		if parseOrderRefFromMessage("batalkan pesanan") != "" {
			return fmt.Errorf("batalkan pesanan should not parse ref")
		}
		if !ShouldCancelPersistedOrder("batalkan pesanan") {
			return fmt.Errorf("batalkan pesanan should trigger persisted cancel path")
		}
		return nil
	})
	addPure("cancel_pick_ref", "ref_parsed", func() error {
		ref := parseOrderRefFromMessage("batalkan WB-947FC5C0")
		if ref != "WB-947FC5C0" {
			return fmt.Errorf("ref = %q", ref)
		}
		return nil
	})
	for _, msg := range []string{"batal", "cancel", "batalkan"} {
		msg := msg
		addPure("cancel_pick_ref", "draft_"+msg, func() error {
			if !IsDraftOrderCancelRequest(msg) {
				return fmt.Errorf("draft cancel %q", msg)
			}
			return nil
		})
	}
	addPure("cancel_pick_ref", "clarify_not_cancel", func() error {
		msg := "order mana yang kamu batalkan ?"
		if IsDraftOrderCancelRequest(msg) {
			return fmt.Errorf("clarification must not be cancel")
		}
		return nil
	})
	addPure("cancel_pick_ref", "order_list_label", func() error {
		o := persistedOrder{
			ID:     "947fc5c0-28b5-471b-bbcb-a19e118aa688",
			Status: "draft",
			ItemsJSON: []byte(`[{"name":"Boxer Mono Spot"}]`),
		}
		label := orderShortLabel(o)
		if !strings.Contains(label, "WB-947FC5C0") {
			return fmt.Errorf("label missing ref: %q", label)
		}
		return nil
	})

	// ── 4. Simulator routing (4) ──
	add("sim_routing", "halo_punya_pesanan", func(sim *ConversationSimulator) []TurnOutcome {
		return []TurnOutcome{sim.Turn("halo min apakah saya punya pesanan aktif ?")}
	}, func(_ orderStatusBuyerCase, o []TurnOutcome) error {
		if o[0].Path != PathOrderStatus {
			return fmt.Errorf("want order_status got %s", o[0].Path)
		}
		return nil
	})
	add("sim_routing", "punya_pesanan_nggak", func(sim *ConversationSimulator) []TurnOutcome {
		return []TurnOutcome{sim.Turn("saya punya pesanan nggak ?")}
	}, func(_ orderStatusBuyerCase, o []TurnOutcome) error {
		if o[0].Path != PathOrderStatus {
			return fmt.Errorf("want order_status got %s", o[0].Path)
		}
		return nil
	})
	add("sim_routing", "pure_halo_still_greeting", func(sim *ConversationSimulator) []TurnOutcome {
		return []TurnOutcome{sim.Turn("halo min")}
	}, func(_ orderStatusBuyerCase, o []TurnOutcome) error {
		if o[0].Path != PathGreeting {
			return fmt.Errorf("pure halo should greet, got %s", o[0].Path)
		}
		return nil
	})
	add("sim_routing", "siang_still_greeting", func(sim *ConversationSimulator) []TurnOutcome {
		return []TurnOutcome{sim.Turn("siang")}
	}, func(_ orderStatusBuyerCase, o []TurnOutcome) error {
		if o[0].Path != PathGreeting {
			return fmt.Errorf("siang alone should greet, got %s", o[0].Path)
		}
		return nil
	})

	// ── 5. Production replay (2) ──
	addPure("production", "pending_not_cancelled_old", func() error {
		msg := "apa saya masih punya pesanan yang pending ya ?"
		if !WantsActiveOrderOnly(msg) {
			return fmt.Errorf("pending inquiry must be active-only")
		}
		if IsGreetingLike(msg) {
			return fmt.Errorf("must not be greeting")
		}
		return nil
	})
	addPure("production", "pick_list_format", func() error {
		reply := orderCancelPickRefReply([]persistedOrder{
			{ID: "947fc5c0-28b5-471b-bbcb-a19e118aa688", Status: "draft", ItemsJSON: []byte(`[{"name":"Mono Spot"}]`)},
			{ID: "eaa94534-1758-4cbe-830c-b2ba16244b0c", Status: "processing", ItemsJSON: []byte(`[{"name":"Hello Kitty"}]`)},
		})
		if !strings.Contains(reply, "WB-947FC5C0") || !strings.Contains(reply, "WB-EAA94534") {
			return fmt.Errorf("pick list missing refs: %s", reply)
		}
		return nil
	})

	if len(out) != 30 {
		panic(fmt.Sprintf("order status buyer cases: want 30 got %d", len(out)))
	}
	return out
}

func truncStatusName(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, " ", "_"), "?", "")
	if len(s) > 42 {
		return s[:42]
	}
	return s
}
