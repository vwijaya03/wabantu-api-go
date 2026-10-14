package buyerflow

import (
	"strings"
	"testing"
)

func TestRoutingParityCheckoutLomba(t *testing.T) {
	cat := "Pengiriman"
	kbETA := []KBEntry{{
		Question: "berapa lama pengiriman",
		Answer:   "Estimasi pengiriman 2-3 hari kerja via JNE.",
		Category: &cat,
		IsActive: true,
	}}

	type caseRow struct {
		name         string
		setup        func() *Simulator
		msg          string
		wantPath     string
		wantKeepCart bool
		wantSub      []string
		wantNotSub   []string
	}

	rows := []caseRow{
		{
			name: "eta_ask_recipient_keeps_cart",
			setup: func() *Simulator {
				s := newWB239488Sim()
				s.KB = kbETA
				return s
			},
			msg:          "kapan barang sampai kak?",
			wantPath:     PathShippingFAQ,
			wantKeepCart: true,
			wantSub:      []string{"nama penerima", "no HP"},
			wantNotSub:   []string{"alamat lengkap (kecamatan"},
		},
		{
			name: "ongkir_ask_recipient_keeps_cart",
			setup: func() *Simulator {
				return newWB239488Sim()
			},
			msg:          "berapa ongkir ke bandung?",
			wantPath:     PathShippingFAQ,
			wantKeepCart: true,
			wantSub:      []string{"nama penerima", "no HP"},
			wantNotSub:   []string{"kecamatan/kota) biar bisa dihitung"},
		},
		{
			name: "negative_promo_not_shipping_faq",
			setup: func() *Simulator {
				return newWB239488Sim()
			},
			msg: "sampai kapan promonya?",
		},
		{
			name: "wb_ref_spelling_newline_plus_address",
			setup: func() *Simulator {
				return newWB239488Sim()
			},
			msg:          "WB-239488D0\n\nJl Sudirman No 10, Jakarta Pusat 10110",
			wantPath:     PathOrderFlow,
			wantKeepCart: true,
		},
		{
			name: "two_refs_pick_one_no_cart_corrupt",
			setup: func() *Simulator {
				return newWB239488Sim()
			},
			msg:          "WB-239488D0 dan WB-00000001\nJl Sudirman No 10 Jakarta",
			wantPath:     PathOrderFlow,
			wantKeepCart: true,
			wantSub:      []string{"WB-00000001", "WB-239488D0", "Pilih"},
		},
		{
			name: "ref_out_of_scope_not_found",
			setup: func() *Simulator {
				return newWB239488Sim()
			},
			msg:          "WB-00000001\nJl Sudirman No 10 Jakarta",
			wantPath:     PathOrderFlow,
			wantKeepCart: true,
			wantSub:      []string{"tidak ditemukan"},
		},
		{
			name: "paid_ref_plus_address_keeps_active_draft_cart",
			setup: func() *Simulator {
				s := newWB239488Sim()
				paidRef := "WB-AABBCCDD"
				s.SimOrderRefStatus = map[string]string{paidRef: "paid"}
				return s
			},
			msg:          "WB-AABBCCDD\nJl Sudirman No 10, Jakarta Pusat",
			wantPath:     PathOrderFlow,
			wantKeepCart: true,
			wantSub:      []string{"sudah dibayar", "belum bisa diubah"},
		},
		{
			name: "completed_ref_plus_address_keeps_active_draft_cart",
			setup: func() *Simulator {
				s := newWB239488Sim()
				doneRef := "WB-CCDDEEFF"
				s.SimOrderRefStatus = map[string]string{doneRef: "completed"}
				return s
			},
			msg:          "WB-CCDDEEFF\nJl Merdeka No 5, Bandung",
			wantPath:     PathOrderFlow,
			wantKeepCart: true,
			wantSub:      []string{"selesai", "belum bisa diubah"},
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			sim := row.setup()
			beforeItems := 0
			if sim.Order != nil {
				beforeItems = len(sim.Order.Items)
			}
			out := sim.Turn(row.msg)
			if row.name == "negative_promo_not_shipping_faq" && out.Path == PathShippingFAQ {
				t.Fatalf("promo ETA must not be shipping_faq, path=%s reply=%q", out.Path, out.Reply)
			}
			if row.wantPath != "" && out.Path != row.wantPath {
				t.Fatalf("path=%q want %q reply=%q", out.Path, row.wantPath, out.Reply)
			}
			if row.wantKeepCart {
				if out.Order == nil {
					t.Fatal("cart cleared")
				}
				if beforeItems > 0 && len(out.Order.Items) != beforeItems {
					t.Fatalf("cart lines changed: before=%d after=%d", beforeItems, len(out.Order.Items))
				}
				if out.BrokeFlow {
					t.Fatal("flow broke / cart cleared flag")
				}
			}
			low := strings.ToLower(out.Reply)
			for _, sub := range row.wantSub {
				if !strings.Contains(strings.ToLower(out.Reply), strings.ToLower(sub)) &&
					!strings.Contains(low, strings.ToLower(sub)) {
					t.Fatalf("reply missing %q: %q", sub, out.Reply)
				}
			}
			for _, sub := range row.wantNotSub {
				if strings.Contains(strings.ToLower(out.Reply), strings.ToLower(sub)) {
					t.Fatalf("reply must not contain %q: %q", sub, out.Reply)
				}
			}
		})
	}
}

func TestIsCheckoutShippingFAQQuestionNegationAndCheckoutData(t *testing.T) {
	if IsCheckoutShippingFAQQuestion("sampai kapan promonya?") {
		t.Fatal("promo negation")
	}
	if IsCheckoutShippingFAQQuestion("kapan sampai?\nJl Sudirman 1") {
		t.Fatal("address must skip intercept")
	}
	if !IsCheckoutShippingFAQQuestion("estimasi sampai kapan ya?") {
		t.Fatal("expected ETA shipping FAQ")
	}
}
