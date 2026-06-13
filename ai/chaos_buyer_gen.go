package ai

import (
	"fmt"
	"strings"
)

// chaosCase — skenario pembeli "kacau": batal tidak reset, order lama vs baru, salah produk.
type chaosCase struct {
	ID       int
	Category string
	Name     string
	Run      func(sim *ConversationSimulator) []TurnOutcome
	Assert   func(c chaosCase, outcomes []TurnOutcome) error
	Pure     func() error
}

func allChaosBuyerCases() []chaosCase {
	var out []chaosCase
	id := 1
	add := func(cat, name string, run func(*ConversationSimulator) []TurnOutcome, assert func(chaosCase, []TurnOutcome) error) {
		out = append(out, chaosCase{ID: id, Category: cat, Name: name, Run: run, Assert: assert})
		id++
	}
	addPure := func(cat, name string, fn func() error) {
		out = append(out, chaosCase{ID: id, Category: cat, Name: name, Pure: fn})
		id++
	}

	// ── 1. Deteksi cancel (25) ──
	for _, w := range []string{"batal", "cancel", "batalkan", "batal dong", "cancel ya", "batalkan pesanan", "batal pesanan", "cancel order"} {
		w := w
		addPure("cancel_detection", w, func() error {
			if !IsDraftOrderCancelRequest(w) {
				return fmt.Errorf("IsDraftOrderCancelRequest(%q) want true", w)
			}
			return nil
		})
	}
	for _, msg := range []string{
		"maaf baru bales, saya ga jadi beli ya kok. apa sudah dibuatkan nomor pesanan untuk saya ?",
		"saya ga jadi beli ya kok. apa sudah dibuatkan nomor pesanan untuk saya ?",
		"ga jadi beli, ada nomor pesanan ga?",
	} {
		msg := msg
		addPure("soft_cancel_status", truncChaosName(msg), func() error {
			if !IsSoftCancelRegret(msg) {
				return fmt.Errorf("IsSoftCancelRegret(%q) want true", msg)
			}
			if ShouldCancelPersistedOrder(msg) {
				return fmt.Errorf("ShouldCancelPersistedOrder(%q) want false", msg)
			}
			return nil
		})
	}
	for _, msg := range []string{"ga jadi deh", "tidak jadi order", "gak jadi beli", "nggak jadi order deh"} {
		msg := msg
		addPure("soft_cancel_alone", truncChaosName(msg), func() error {
			if !IsSoftCancelRegret(msg) {
				return fmt.Errorf("IsSoftCancelRegret(%q) want true", msg)
			}
			if !ShouldCancelPersistedOrder(msg) {
				return fmt.Errorf("ShouldCancelPersistedOrder(%q) want true for standalone regret", msg)
			}
			return nil
		})
	}
	for _, msg := range []string{"batalkan pesanan", "cancel order", "batal pesanan", "batal", "cancel"} {
		msg := msg
		addPure("explicit_persisted_cancel", msg, func() error {
			if !ShouldCancelPersistedOrder(msg) {
				return fmt.Errorf("ShouldCancelPersistedOrder(%q) want true", msg)
			}
			return nil
		})
	}
	for _, msg := range []string{
		"ga jadi mau dirubah menjadi 10 biji ya", "loh ubah jadi 5 paket",
		"pesanan yang atas nama saya ada kah ?", "harga berapa",
	} {
		msg := msg
		addPure("not_cancel", truncChaosName(msg), func() error {
			if IsOrderCancelRequest(msg) && !strings.Contains(msg, "batalkan pesanan") {
				if strings.Contains(msg, "ga jadi mau dirubah") || strings.Contains(msg, "ubah jadi") {
					return fmt.Errorf("IsOrderCancelRequest(%q) want false", msg)
				}
			}
			return nil
		})
	}

	// ── 2. Batal di tengah flow harus reset (20) ──
	for _, step := range []string{"ask_variant", "ask_qty", "ask_recipient"} {
		for _, cm := range []string{"batal", "cancel", "batalkan"} {
			step, cm := step, cm
			add("batal_reset", step+"_"+cm, func(sim *ConversationSimulator) []TurnOutcome {
				sim.Order = baseOrderBoxer(2, step)
				return []TurnOutcome{sim.Turn(cm)}
			}, func(_ chaosCase, o []TurnOutcome) error {
				last := o[len(o)-1]
				if !last.Canceled {
					return fmt.Errorf("want canceled at %s with %q", step, cm)
				}
				if last.Order != nil {
					return fmt.Errorf("order should be nil after cancel")
				}
				return nil
			})
		}
	}
	for _, cm := range []string{"batal pesanan", "cancel order", "ga jadi deh", "tidak jadi order", "batal bang", "cancel deh", "batalkan ya kak", "batal dong", "cancel ya", "tidak jadi deh", "batalin"} {
		cm := cm
		add("batal_reset", "slang_"+truncChaosName(cm), func(sim *ConversationSimulator) []TurnOutcome {
			sim.Order = baseOrderBoxer(1, "ask_recipient")
			return []TurnOutcome{sim.Turn(cm)}
		}, func(_ chaosCase, o []TurnOutcome) error {
			if !o[0].Canceled {
				return fmt.Errorf("%q should cancel draft", cm)
			}
			return nil
		})
	}

	// ── 3. Cancel lalu order baru — produk benar (15) ──
	for i, msg := range []string{
		"mau pesen boxer pria mono spot 5 paket ya",
		"saya mau beli 5 paket boxer mono spot",
		"mau pesan boxer mono spot 3 paket",
	} {
		msg := msg
		add("cancel_restart", fmt.Sprintf("mono_%d", i), func(sim *ConversationSimulator) []TurnOutcome {
			sim.Order = baseOrderBoxer(2, "ask_recipient")
			sim.Turn("cancel")
			return []TurnOutcome{sim.Turn(msg)}
		}, func(_ chaosCase, o []TurnOutcome) error {
			last := o[len(o)-1]
			if last.Order == nil || !strings.Contains(strings.ToLower(last.Order.ProductName), "mono spot") {
				return fmt.Errorf("want mono spot after restart, got %+v", last.Order)
			}
			return nil
		})
	}
	for i := 0; i < 12; i++ {
		i := i
		add("cancel_restart", fmt.Sprintf("qty_%d", i), func(sim *ConversationSimulator) []TurnOutcome {
			sim.Order = baseOrderBoxer(1, "ask_qty")
			sim.Turn("batal")
			return []TurnOutcome{sim.Turn(fmt.Sprintf("mau %d paket boxer mono spot", (i%5)+1))}
		}, func(_ chaosCase, o []TurnOutcome) error {
			last := o[len(o)-1]
			if last.Order == nil || !strings.Contains(strings.ToLower(last.Order.ProductName), "mono spot") {
				return fmt.Errorf("bad restart product: %+v", last.Order)
			}
			return nil
		})
	}

	// ── 4. Disambiguasi produk (20) ──
	for _, pc := range []struct{ msg, want string }{
		{"mau pesen boxer pria mono spot 5 paket ya", "mono spot"},
		{"saya mau beli 5 paket boxer mono spot", "mono spot"},
		{"boxer pria mono spot bukan hello kitty", "mono spot"},
		{"mau hello kitty anak perempuan 2 pcs", "hello kitty"},
		{"boxer anak perempuan hello kitty", "hello kitty"},
		{"mau de wasa 3 paket", "de wasa"},
		{"celana dalam pria de wasa 2 paket", "de wasa"},
	} {
		pc := pc
		addPure("product_match", truncChaosName(pc.msg), func() error {
			m := matchCatalogItem(pc.msg, omahCatalog())
			if m == nil {
				return fmt.Errorf("no match for %q", pc.msg)
			}
			if !strings.Contains(strings.ToLower(m.Name), pc.want) {
				return fmt.Errorf("got %q want %q", m.Name, pc.want)
			}
			return nil
		})
	}
	for i, msg := range []string{
		"mau pesen boxer pria mono spot 5 paket ya",
		"saya mau beli 5 paket boxer mono spot",
		"order abon 3 biji",
		"mau de wasa 2 paket",
		"hello kitty anak 1 pcs",
	} {
		msg := msg
		add("product_flow", fmt.Sprintf("flow_%d", i), func(sim *ConversationSimulator) []TurnOutcome {
			return []TurnOutcome{sim.Turn(msg)}
		}, func(c chaosCase, o []TurnOutcome) error {
			last := o[len(o)-1]
			if last.Order == nil {
				return fmt.Errorf("expected order for %q", msg)
			}
			return nil
		})
	}
	add("product_correction", "bukan_hello_kitty", func(sim *ConversationSimulator) []TurnOutcome {
		st := baseOrderBoxer(5, "ask_recipient")
		st.ProductName = "1PCS CELANA DALAM BOXER ANAK PEREMPUAN MOTIF HELLO KITTY BUNGA LEMBUT - L"
		st.CatalogItemID = "hello-kitty-l"
		sim.Order = st
		return []TurnOutcome{sim.Turn("boxer pria mono spot bukan hello kitty")}
	}, func(_ chaosCase, o []TurnOutcome) error {
		last := o[len(o)-1]
		if last.Order == nil || strings.Contains(strings.ToLower(last.Order.ProductName), "hello kitty") {
			return fmt.Errorf("product not corrected: %+v", last.Order)
		}
		if !strings.Contains(strings.ToLower(last.Order.ProductName), "mono spot") {
			return fmt.Errorf("want mono spot got %q", last.Order.ProductName)
		}
		return nil
	})
	for i := 0; i < 8; i++ {
		i := i
		addPure("product_pri_boost", fmt.Sprintf("pria_%d", i), func() error {
			msg := fmt.Sprintf("boxer pria mono spot %d paket", (i%3)+1)
			m := matchCatalogItem(msg, omahCatalog())
			if m == nil || !strings.Contains(strings.ToLower(m.Name), "mono spot") {
				return fmt.Errorf("pria→mono spot failed for %q", msg)
			}
			return nil
		})
	}

	// ── 5. Replay transcript (10) ──
	add("transcript_replay", "full_chaos", func(sim *ConversationSimulator) []TurnOutcome {
		return sim.RunScript(
			"mau pesen boxer pria mono spot 5 paket ya",
			"boxer pria mono spot bukan hello kitty",
			"batal",
			"cancel",
			"saya mau beli 5 paket boxer mono spot",
		)
	}, func(_ chaosCase, o []TurnOutcome) error {
		if !o[2].Canceled {
			return fmt.Errorf("batal should cancel draft")
		}
		last := o[len(o)-1]
		if last.Order == nil || !strings.Contains(strings.ToLower(last.Order.ProductName), "mono spot") {
			return fmt.Errorf("final should be mono spot: %+v", last.Order)
		}
		return nil
	})
	for _, tp := range []struct {
		name string
		msgs []string
		fn   func([]TurnOutcome) error
	}{
		{"batal_then_cancel", []string{"mau pesen boxer mono spot 2 paket", "batal", "cancel"}, func(o []TurnOutcome) error {
			if !o[1].Canceled {
				return fmt.Errorf("batal should cancel")
			}
			return nil
		}},
		{"cancel_new_same", []string{"mau pesen boxer pria mono spot 5 paket ya", "cancel", "mau pesen boxer pria mono spot 5 paket ya"}, func(o []TurnOutcome) error {
			if !strings.Contains(strings.ToLower(o[2].Order.ProductName), "mono spot") {
				return fmt.Errorf("second order wrong product")
			}
			return nil
		}},
		{"correction_cancel", []string{"mau pesen boxer pria mono spot 3 paket", "boxer pria mono spot bukan hello kitty", "batal"}, func(o []TurnOutcome) error {
			if !o[2].Canceled {
				return fmt.Errorf("batal after correction should cancel")
			}
			return nil
		}},
		{"double_batal", []string{"mau pesen boxer mono spot 1 paket", "batal", "batal"}, func(o []TurnOutcome) error {
			if !o[1].Canceled {
				return fmt.Errorf("first batal should cancel")
			}
			return nil
		}},
		{"cancel_abon_restart", []string{"order abon 2 biji", "cancel", "order abon 5 biji"}, func(o []TurnOutcome) error {
			if o[2].Order == nil || o[2].Order.Qty != 5 {
				return fmt.Errorf("want abon qty 5 got %+v", o[2].Order)
			}
			return nil
		}},
		{"batal_abon_restart", []string{"mau 3 biji abon", "batal", "mau 1 biji abon"}, func(o []TurnOutcome) error {
			if o[2].Order == nil {
				return fmt.Errorf("expected abon order")
			}
			return nil
		}},
		{"mono_not_hello", []string{"mau pesen boxer pria mono spot 5 paket ya"}, func(o []TurnOutcome) error {
			if strings.Contains(strings.ToLower(o[0].Order.ProductName), "hello kitty") {
				return fmt.Errorf("should not pick hello kitty")
			}
			return nil
		}},
		{"revision_not_batal", []string{"mau pesen boxer mono spot 2 paket", "ga jadi mau dirubah menjadi 5 biji ya"}, func(o []TurnOutcome) error {
			if o[1].Canceled {
				return fmt.Errorf("revision should not cancel")
			}
			return nil
		}},
		{"explicit_words", []string{"batal", "cancel", "batalkan"}, func(o []TurnOutcome) error {
			return nil
		}},
	} {
		tp := tp
		if tp.name == "explicit_words" {
			for _, w := range tp.msgs {
				w := w
				addPure("transcript_replay", "word_"+w, func() error {
					if !IsDraftOrderCancelRequest(w) {
						return fmt.Errorf("%q not draft cancel", w)
					}
					return nil
				})
			}
			continue
		}
		add("transcript_replay", tp.name, func(sim *ConversationSimulator) []TurnOutcome {
			return sim.RunScript(tp.msgs...)
		}, func(_ chaosCase, o []TurnOutcome) error {
			return tp.fn(o)
		})
	}

	for len(out) < 100 {
		n := len(out) + 1
		qty := (n % 8) + 1
		add("cancel_restart_pad", fmt.Sprintf("pad_%d", n), func(sim *ConversationSimulator) []TurnOutcome {
			sim.Order = baseOrderBoxer(1, "ask_recipient")
			sim.Turn("batal")
			return []TurnOutcome{sim.Turn(fmt.Sprintf("order abon %d biji", qty))}
		}, func(_ chaosCase, o []TurnOutcome) error {
			last := o[len(o)-1]
			if last.Order == nil || !strings.Contains(strings.ToLower(last.Order.ProductName), "abon") {
				return fmt.Errorf("want abon order")
			}
			return nil
		})
	}
	if len(out) > 100 {
		out = out[:100]
	}
	return out
}

func truncChaosName(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, " ", "_"), "?", "")
	if len(s) > 36 {
		return s[:36]
	}
	return s
}
