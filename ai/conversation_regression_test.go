package ai

import (
	"strings"
	"testing"
)

// conversationRegressionCase — skenario regresi dari percakapan WA nyata (Omah Apparel thread Jul 2026).
type conversationRegressionCase struct {
	name       string
	input      string
	wantPath   string
	wantSubstr []string
	wantNot    []string
	extraCheck func(t *testing.T, out TurnOutcome)
}

var conversationRegressionCases = []conversationRegressionCase{
	{
		name:     "rekening_from_kb_not_catalog",
		input:    "bisa minta nomor rekeningnya ga sih ?",
		wantPath: PathPaymentFAQ,
		wantSubstr: []string{"BCA", "110220330"},
		wantNot:  []string{"belum menemukan data tersebut di katalog"},
	},
	{
		name:     "cara_bayar_from_kb",
		input:    "saya bayarnya gimana ini ?",
		wantPath: PathPaymentFAQ,
		wantSubstr: []string{"BCA"},
		wantNot:  []string{"tunggu konfirmasi CS"},
	},
	{
		name:     "bare_order_ref_status",
		input:    "WB-58D662BC",
		wantPath: PathOrderStatus,
		wantNot:  []string{"di luar topik bisnis"},
	},
	{
		name:     "detail_pesanan_with_ref",
		input:    "mau lihat detail pesanan WB-58D662BC",
		wantPath: PathOrderStatus,
	},
	{
		name:     "status_pesanan_with_ref",
		input:    "gimana status nya pesanan WB-372AF9ED",
		wantPath: PathOrderStatus,
	},
	{
		name:     "pending_orders_list",
		input:    "saya masih ada pesanan pending ga ya ?",
		wantPath: PathOrderStatus,
	},
	{
		name:  "payment_proof_resubmit_caption",
		input: "coba cek lagi min, ini saya kirim lagi",
		extraCheck: func(t *testing.T, _ TurnOutcome) {
			t.Helper()
			if !IsPaymentProofInbound("image", "coba cek lagi min, ini saya kirim lagi") {
				t.Fatal("resubmit image caption must skip autoreply (payment proof pipeline)")
			}
		},
	},
	{
		name:     "best_seller_catalog_not_payment",
		input:    "best seller di toko ini apa ?",
		wantPath: PathCatalogDB,
		wantSubstr: []string{"Abon"},
	},
}

// TestConversationRegression — loop regresi cepat dari bug percakapan nyata.
// Jalankan: ./scripts/run-ai-regression-tests.sh
func TestConversationRegression(t *testing.T) {
	sim := newOmahSimulator()
	for _, tc := range conversationRegressionCases {
		t.Run(tc.name, func(t *testing.T) {
			local := newOmahSimulator()
			local.History = append([]dbMessage{}, sim.History...)
			local.Order = sim.Order
			out := local.Turn(tc.input)
			if tc.extraCheck != nil {
				tc.extraCheck(t, out)
				return
			}
			if tc.wantPath != "" && out.Path != tc.wantPath {
				t.Fatalf("path = %q want %q reply=%q", out.Path, tc.wantPath, out.Reply)
			}
			lower := strings.ToLower(out.Reply)
			for _, s := range tc.wantSubstr {
				if !strings.Contains(strings.ToUpper(out.Reply), strings.ToUpper(s)) && !strings.Contains(lower, strings.ToLower(s)) {
					t.Fatalf("reply missing %q: %q", s, out.Reply)
				}
			}
			for _, s := range tc.wantNot {
				if strings.Contains(lower, strings.ToLower(s)) {
					t.Fatalf("reply must not contain %q: %q", s, out.Reply)
				}
			}
		})
	}
}

// TestConversationRegressionScript — alur multi-turn order Abon (ringkasan dari thread nyata).
func TestConversationRegressionScript(t *testing.T) {
	sim := newOmahSimulator()
	script := []struct {
		input    string
		wantPath string
	}{
		{"mau order abon sapi yang 500 gram 3 biji ya", PathOrderFlow},
		{"Nama: Suciati\nHP: 081222333090", PathOrderFlow},
	}
	for i, step := range script {
		out := sim.Turn(step.input)
		if out.Path != step.wantPath {
			t.Fatalf("step %d path=%q want %q", i, out.Path, step.wantPath)
		}
	}
}
