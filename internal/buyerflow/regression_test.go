package buyerflow

import (
	"strings"
	"testing"
)

// TestRegression — golden loop percakapan WA (tanpa Encore/DB).
//
// Menambah case baru:
//  1. Tambah entry di regression_cases.go
//  2. Jalankan: go test ./internal/buyerflow/ -run TestRegression -v
//  3. PR: check AI Regression (fast) wajib hijau (<10s)
func TestRegression(t *testing.T) {
	sim := NewOmahSimulator()
	for _, tc := range regressionCases {
		t.Run(tc.name, func(t *testing.T) {
			local := NewOmahSimulator()
			local.History = append([]Message{}, sim.History...)
			local.Order = sim.Order
			for _, prior := range tc.priorInputs {
				local.Turn(prior)
			}
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

func TestRegressionScript(t *testing.T) {
	sim := NewOmahSimulator()
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

func TestRegressionShippingScript(t *testing.T) {
	sim := NewOmahSimulator()
	steps := []struct {
		input    string
		wantPath string
	}{
		{"mau tanya produk dulu", PathCatalogDB},
		{"berapa ongkir ke bandung?", PathShippingFAQ},
		{"berapa lama pengiriman?", PathShippingFAQ},
	}
	for i, step := range steps {
		out := sim.Turn(step.input)
		if out.Path != step.wantPath {
			t.Fatalf("step %d path=%q want %q reply=%q", i, out.Path, step.wantPath, out.Reply)
		}
	}
}

func TestRegressionOrderRevisionScript(t *testing.T) {
	sim := NewOmahSimulator()
	out := sim.Turn("mau beli abon sapi 2 pcs")
	if out.Path != PathOrderFlow {
		t.Fatalf("setup order path=%q", out.Path)
	}
	rev := sim.Turn("revisi jadi 5 pcs")
	if rev.Path != PathOrderFlow {
		t.Fatalf("revision path=%q want %q reply=%q", rev.Path, PathOrderFlow, rev.Reply)
	}
}

func TestTryPaymentFAQAnswer(t *testing.T) {
	cat := "Nomor Rekening"
	kb := []KBEntry{{
		Question: "Nomor Rekening",
		Answer:   "BCA 110220330 atas nama Omah Apparel",
		Category: &cat,
		IsActive: true,
	}}
	ans, ok := TryPaymentFAQAnswer("bisa minta nomor rekeningnya ga sih ?", kb)
	if !ok {
		t.Fatal("expected payment FAQ match")
	}
	if !strings.Contains(strings.ToUpper(ans), "BCA") {
		t.Fatalf("unexpected answer: %q", ans)
	}
	_, ok = TryPaymentFAQAnswer("best seller apa?", kb)
	if ok {
		t.Fatal("non-payment question should not match payment FAQ")
	}
}

func TestIsOrderRefStatusLookup(t *testing.T) {
	if !IsOrderRefStatusLookup("WB-58D662BC") {
		t.Fatal("bare order ref should trigger status lookup")
	}
	if !IsOrderRefStatusLookup("mau lihat detail pesanan WB-58D662BC") {
		t.Fatal("detail pesanan with ref should trigger status lookup")
	}
	if IsOrderRefStatusLookup("halo kak") {
		t.Fatal("greeting should not trigger order ref lookup")
	}
}
