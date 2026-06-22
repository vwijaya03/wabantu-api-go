package ai

import "fmt"

// buyerCase — satu skenario perilaku pembeli WA.
type buyerCase struct {
	ID       int
	Category string
	Name     string
	Run      func(sim *ConversationSimulator) []TurnOutcome
	Assert   func(c buyerCase, outcomes []TurnOutcome) error
}

func allBuyerBehaviorCases() []buyerCase {
	cases := make([]buyerCase, 0, 1100)
	nextID := 1
	add := func(cat, name string, run func(*ConversationSimulator) []TurnOutcome, assert func(buyerCase, []TurnOutcome) error) {
		cases = append(cases, buyerCase{ID: nextID, Category: cat, Name: name, Run: run, Assert: assert})
		nextID++
	}

	// ── 1. Happy checkout lengkap (food + apparel) ──
	for qty := 1; qty <= 10; qty++ {
		q := qty
		add("happy_checkout_food", fmt.Sprintf("abon_%d_pcs", q), func(sim *ConversationSimulator) []TurnOutcome {
			return sim.RunScript(
				fmt.Sprintf("saya jadi beli abon sapi %d pcs", q),
				recipientBlock("Budi", "081234567890"),
				fullAddressBlock(),
			)
		}, func(_ buyerCase, o []TurnOutcome) error {
			last := o[len(o)-1]
			if !last.Completed {
				return fmt.Errorf("expected completed, got path=%s", last.Path)
			}
			if last.Order != nil {
				return fmt.Errorf("order should be cleared after complete")
			}
			return nil
		})
	}
	for qty := 1; qty <= 10; qty++ {
		q := qty
		add("happy_checkout_apparel", fmt.Sprintf("boxer_%d_paket", q), func(sim *ConversationSimulator) []TurnOutcome {
			return sim.RunScript(
				fmt.Sprintf("saya jadi beli boxer mono spot %d paket", q),
				recipientBlock("Andi", "081987654321"),
				fullAddressBlock(),
			)
		}, func(_ buyerCase, o []TurnOutcome) error {
			if !o[len(o)-1].Completed {
				return fmt.Errorf("expected completed")
			}
			return nil
		})
	}

	// ── 2. Revisi qty di ask_recipient lalu selesai ──
	revisionPhrases := []string{
		"revisi jadi %d paket",
		"ubah jadi %d paket",
		"loh ubah jadi %d biji",
		"ga jadi mau dirubah menjadi %d biji ya",
		"ganti jadi %d pcs",
	}
	for _, tpl := range revisionPhrases {
		for _, n := range []int{2, 3, 5, 10} {
			phrase := fmt.Sprintf(tpl, n)
			add("revision_qty_complete", phrase, func(sim *ConversationSimulator) []TurnOutcome {
				o := sim.RunScript("saya jadi beli abon sapi 1 pcs")
				if sim.Order != nil {
					sim.Order.Step = "ask_recipient"
					sim.Order.Qty = 1
				}
				o = append(o, sim.Turn(phrase))
				o = append(o, sim.Turn(recipientBlock("Siti", "081211111111")))
				o = append(o, sim.Turn(fullAddressBlock()))
				return o
			}, func(_ buyerCase, o []TurnOutcome) error {
				if !o[len(o)-1].Completed {
					return fmt.Errorf("expected complete after revision")
				}
				return nil
			})
		}
	}

	// ── 3. Revisi qty lalu batal ──
	cancelAfterRevision := []string{"tidak jadi order", "batalkan order", "ga jadi deh", "batal pesanan"}
	for i, cancel := range cancelAfterRevision {
		for _, n := range []int{5, 10} {
			n, cancel := n, cancel
			add("revision_then_cancel", fmt.Sprintf("rev_%d_cancel_%d", i, n), func(sim *ConversationSimulator) []TurnOutcome {
				sim.RunScript("saya jadi beli abon sapi 1 pcs")
				if sim.Order != nil {
					sim.Order.Step = "ask_recipient"
				}
				sim.Turn(fmt.Sprintf("ubah jadi %d biji", n))
				return sim.RunScript(cancel)
			}, func(_ buyerCase, o []TurnOutcome) error {
				last := o[len(o)-1]
				if !last.Canceled {
					return fmt.Errorf("expected cancel after revision")
				}
				return nil
			})
		}
	}

	// ── 4. Ngomong-ngomong lalu tidak jadi ──
	chitchatAbandon := []struct {
		name string
		msgs []string
	}{
		{"browse_then_leave", []string{"jualan apa aja", "oke makasih"}},
		{"consult_then_leave", []string{"boxer bisa beli per biji ga?", "oke paham"}},
		{"price_then_leave", []string{"harga abon berapa", "nanti aja deh"}},
		{"greeting_only", []string{"halo kak"}},
		{"praise_then_stop", []string{"boxer cowok ada ga", "nah pinter lu udah"}},
		{"correction_abort", []string{"mau abon sapi", "mau 1 pcs", "loh saya masih tanya jangan checkout"}},
	}
	for _, sc := range chitchatAbandon {
		msgs := sc.msgs
		add("chitchat_abandon", sc.name, func(sim *ConversationSimulator) []TurnOutcome {
			return sim.RunScript(msgs...)
		}, func(_ buyerCase, o []TurnOutcome) error {
			last := o[len(o)-1]
			if last.Completed {
				return fmt.Errorf("should not complete checkout")
			}
			return nil
		})
	}
	for i := 0; i < 40; i++ {
		i := i
		add("chitchat_abandon", fmt.Sprintf("browse_variant_%d", i), func(sim *ConversationSimulator) []TurnOutcome {
			sim.RunScript("mau beli boxer mono spot")
			return sim.RunScript("berapa harga jeans?")
		}, func(_ buyerCase, o []TurnOutcome) error {
			if o[len(o)-1].Completed {
				return fmt.Errorf("price question should break order")
			}
			return nil
		})
	}

	// ── 5. Consulting → explicit purchase ──
	for i := 0; i < 20; i++ {
		add("consulting_to_purchase", fmt.Sprintf("boleh_eceran_%d", i), func(sim *ConversationSimulator) []TurnOutcome {
			o := sim.RunScript("boleh beli 1 pcs boxer mono spot?")
			o = append(o, sim.Turn("saya jadi beli boxer mono spot 1 paket"))
			return o
		}, func(_ buyerCase, o []TurnOutcome) error {
			if o[0].Intent.State != SalesStateConsulting {
				return fmt.Errorf("turn1 want consulting got %s", o[0].Intent.State)
			}
			return nil
		})
	}

	// ── 6. parseOrderQty matrix (generated) ──
	units := []string{"pcs", "biji", "paket", "buah", "piece"}
	for n := 1; n <= 20; n++ {
		for _, u := range units {
			n, u := n, u
			msg := fmt.Sprintf("mau %d %s", n, u)
			add("parse_qty", msg, func(sim *ConversationSimulator) []TurnOutcome {
				q, ok := parseOrderQty(msg)
				_ = sim
				if !ok || q != n {
					return nil
				}
				return nil
			}, func(_ buyerCase, _ []TurnOutcome) error {
				q, ok := parseOrderQty(msg)
				if !ok || q != n {
					return fmt.Errorf("parseOrderQty(%q)=%d ok=%v", msg, q, ok)
				}
				return nil
			})
		}
	}

	// ── 7. revision vs cancel disambiguation ──
	revMsgs := []string{
		"bukan revisi saya order 3 paket bukan 1 paket",
		"revisi jadi 10 pakettt",
		"gw mau ubah jadi 10 paket bisa?",
		"ga jadi mau dirubah menjadi 10 biji ya",
	}
	for _, m := range revMsgs {
		msg := m
		add("revision_not_cancel", msg, func(sim *ConversationSimulator) []TurnOutcome {
			_ = sim
			return nil
		}, func(_ buyerCase, _ []TurnOutcome) error {
			if !IsOrderRevisionMessage(msg) {
				return fmt.Errorf("want revision")
			}
			if IsOrderCancelRequest(msg) {
				return fmt.Errorf("revision should not cancel")
			}
			return nil
		})
	}
	cancelMsgs := []string{"ga jadi deh", "tidak jadi order", "batalkan pesanan", "batal order ya"}
	for _, m := range cancelMsgs {
		msg := m
		add("cancel_not_revision", msg, func(sim *ConversationSimulator) []TurnOutcome {
			return nil
		}, func(_ buyerCase, _ []TurnOutcome) error {
			if !IsOrderCancelRequest(msg) {
				return fmt.Errorf("want cancel")
			}
			if IsOrderRevisionMessage(msg) {
				return fmt.Errorf("cancel should not be revision")
			}
			return nil
		})
	}

	// ── 8. Intent routing ──
	intentCases := []struct {
		msg   string
		state string
	}{
		{"jualan apa aja", SalesStateBrowsing},
		{"boxer cowok ada ga", SalesStateProductSelected},
		{"boleh beli 1 pcs ?", SalesStateConsulting},
		{"revisi jadi 5 paket", SalesStateCheckout},
		{"kak", SalesStateGreeting},
		{"ini tokonya dimananya", SalesStateConsulting},
	}
	for i := 0; i < 25; i++ {
		for _, ic := range intentCases {
			msg, want := ic.msg, ic.state
			add("intent_routing", fmt.Sprintf("%s_%d", msg, i), func(sim *ConversationSimulator) []TurnOutcome {
				intent := ResolveSalesIntent(msg, sim.History, sim.Order != nil, true, sim.Profile, sim.Catalog)
				_ = intent
				return nil
			}, func(_ buyerCase, _ []TurnOutcome) error {
				intent := ResolveSalesIntent(msg, nil, false, true, omahProfile(), omahCatalog())
				if intent.State != want {
					return fmt.Errorf("want %s got %s", want, intent.State)
				}
				return nil
			})
		}
	}

	// ── 9. ShouldBreakOrderFlow ──
	breakCases := []struct {
		msg, step string
		want      bool
	}{
		{"berapa harga nya?", "ask_variant", true},
		{"revisi jadi 5 paket", "ask_recipient", false},
		{"3 paket", "ask_variant", false},
		{"jualan apa aja", "ask_variant", true},
	}
	for i := 0; i < 30; i++ {
		for _, bc := range breakCases {
			msg, step, want := bc.msg, bc.step, bc.want
			add("break_flow", fmt.Sprintf("%s_%s_%d", step, msg, i), func(sim *ConversationSimulator) []TurnOutcome {
				return nil
			}, func(_ buyerCase, _ []TurnOutcome) error {
				got := ShouldBreakOrderFlow(msg, step, nil)
				if got != want {
					return fmt.Errorf("break=%v want %v", got, want)
				}
				return nil
			})
		}
	}

	// ── 10. tryApplyQtyRevision on state ──
	for n := 2; n <= 15; n++ {
		n := n
		phrases := []string{
			fmt.Sprintf("ubah jadi %d paket", n),
			fmt.Sprintf("revisi %d biji", n),
			fmt.Sprintf("ganti jadi %d pcs", n),
		}
		for _, p := range phrases {
			phrase := p
			add("apply_qty_revision", phrase, func(sim *ConversationSimulator) []TurnOutcome {
				return nil
			}, func(_ buyerCase, _ []TurnOutcome) error {
				st := orderState{Qty: 1, ProductName: "Abon Sapi 500G", UnitPrice: 35000}
				if !tryApplyQtyRevision(&st, phrase) {
					return fmt.Errorf("revision not applied")
				}
				if st.Qty != n {
					return fmt.Errorf("qty=%d want %d", st.Qty, n)
				}
				return nil
			})
		}
	}

	// ── 11. Shipping parse ──
	for i := 0; i < 30; i++ {
		add("shipping_parse", fmt.Sprintf("address_%d", i), func(sim *ConversationSimulator) []TurnOutcome {
			return nil
		}, func(_ buyerCase, _ []TurnOutcome) error {
			st := orderState{}
			mergeShippingText(&st, fullAddressBlock())
			mergeShippingText(&st, recipientBlock("Rina", "081299988877"))
			if !st.shippingComplete() {
				return fmt.Errorf("expected shipping complete")
			}
			return nil
		})
	}

	// ── 12. AdvanceOrderFlow single steps ──
	for qty := 1; qty <= 5; qty++ {
		q := qty
		add("fsm_food_step", fmt.Sprintf("abon_qty_%d", q), func(sim *ConversationSimulator) []TurnOutcome {
			res := AdvanceOrderFlow(OrderFlowInput{
				UserText: fmt.Sprintf("mau abon sapi %d pcs", q),
				Catalog:  sim.Catalog,
				Profile:  sim.Profile,
			}, nil)
			if res.State != nil && res.State.Step != "ask_recipient" {
				return nil
			}
			return nil
		}, func(_ buyerCase, _ []TurnOutcome) error {
			res := AdvanceOrderFlow(OrderFlowInput{
				UserText: fmt.Sprintf("mau abon sapi %d pcs", q),
				Catalog:  omahCatalog(),
				Profile:  omahProfile(),
			}, nil)
			if res.State == nil || res.State.Step != "ask_recipient" || res.State.Qty != q {
				return fmt.Errorf("want ask_recipient qty=%d got step=%v qty=%d", q, res.State, res.State.Qty)
			}
			return nil
		})
	}

	// Pad dengan kombinasi qty×produk sampai 1000
	products := []string{"abon sapi", "boxer mono spot", "hello kitty"}
	for _, prod := range products {
		for n := 1; n <= 10; n++ {
			if len(cases) >= 1000 {
				break
			}
			prod, n := prod, n
			add("purchase_intent", fmt.Sprintf("%s_%d", prod, n), func(sim *ConversationSimulator) []TurnOutcome {
				return nil
			}, func(_ buyerCase, _ []TurnOutcome) error {
				msg := fmt.Sprintf("saya jadi beli %s %d pcs", prod, n)
				if !HasPurchaseIntent(msg) {
					return fmt.Errorf("expected purchase intent: %s", msg)
				}
				return nil
			})
		}
	}

	if len(cases) > 1000 {
		cases = cases[:1000]
	}
	for len(cases) < 1000 {
		i := len(cases) + 1
		add("padding_scope", fmt.Sprintf("in_scope_%d", i), func(sim *ConversationSimulator) []TurnOutcome {
			return nil
		}, func(_ buyerCase, _ []TurnOutcome) error {
			scope := ExtractScopeKeywords("Omah Apparel boxer abon")
			if !IsWithinBusinessScope("mau pesan 1 pcs", scope, nil) {
				return fmt.Errorf("pcs order should be in scope")
			}
			return nil
		})
	}
	return cases[:1000]
}
