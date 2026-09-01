package buyerflow

import (
	"fmt"
	"strings"
	"time"
)

// RegressionCaseResult is one scenario outcome.
type RegressionCaseResult struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Error  string `json:"error,omitempty"`
}

// RegressionSuiteResult groups cases for one suite.
type RegressionSuiteResult struct {
	Name       string                 `json:"name"`
	Passed     bool                   `json:"passed"`
	DurationMs int64                  `json:"durationMs"`
	Cases      []RegressionCaseResult `json:"cases"`
	Skipped    bool                   `json:"skipped,omitempty"`
	SkipReason string                 `json:"skipReason,omitempty"`
}

// RegressionRunResult mirrors scripts/run-ai-regression-tests.sh output.
type RegressionRunResult struct {
	Passed     bool                    `json:"passed"`
	DurationMs int64                   `json:"durationMs"`
	Suites     []RegressionSuiteResult `json:"suites"`
}

type fatalRecorder struct {
	msg string
}

func (f *fatalRecorder) Helper() {}

func (f *fatalRecorder) Fatal(args ...any) {
	if f.msg == "" {
		f.msg = fmt.Sprint(args...)
	}
}

func runRegressionCases(suiteName string, cases []regressionCase, base *Simulator) RegressionSuiteResult {
	start := time.Now()
	out := RegressionSuiteResult{Name: suiteName, Passed: true}
	for _, tc := range cases {
		cr := RegressionCaseResult{Name: tc.name, Passed: true}
		local := NewOmahSimulator()
		if base != nil {
			local.History = append([]Message{}, base.History...)
			local.Order = base.Order
		}
		for _, prior := range tc.priorInputs {
			local.Turn(prior)
		}
		turn := local.Turn(tc.input)
		if tc.extraCheck != nil {
			rec := &fatalRecorder{}
			tc.extraCheck(rec, turn)
			if rec.msg != "" {
				cr.Passed = false
				cr.Error = rec.msg
			}
		} else {
			if tc.wantPath != "" && turn.Path != tc.wantPath {
				cr.Passed = false
				cr.Error = fmt.Sprintf("path = %q want %q reply=%q", turn.Path, tc.wantPath, turn.Reply)
			}
			if cr.Passed {
				lower := strings.ToLower(turn.Reply)
				for _, s := range tc.wantSubstr {
					if !strings.Contains(strings.ToUpper(turn.Reply), strings.ToUpper(s)) && !strings.Contains(lower, strings.ToLower(s)) {
						cr.Passed = false
						cr.Error = fmt.Sprintf("reply missing %q: %q", s, turn.Reply)
						break
					}
				}
			}
			if cr.Passed {
				lower := strings.ToLower(turn.Reply)
				for _, s := range tc.wantNot {
					if strings.Contains(lower, strings.ToLower(s)) {
						cr.Passed = false
						cr.Error = fmt.Sprintf("reply must not contain %q: %q", s, turn.Reply)
						break
					}
				}
			}
		}
		if !cr.Passed {
			out.Passed = false
		}
		out.Cases = append(out.Cases, cr)
	}
	out.DurationMs = time.Since(start).Milliseconds()
	return out
}

func runScriptSuite(name string, steps []struct {
	input    string
	wantPath string
}) RegressionSuiteResult {
	start := time.Now()
	out := RegressionSuiteResult{Name: name, Passed: true}
	sim := NewOmahSimulator()
	for i, step := range steps {
		turn := sim.Turn(step.input)
		cr := RegressionCaseResult{Name: fmt.Sprintf("step_%d", i), Passed: true}
		if turn.Path != step.wantPath {
			cr.Passed = false
			cr.Error = fmt.Sprintf("path=%q want %q reply=%q", turn.Path, step.wantPath, turn.Reply)
			out.Passed = false
		}
		out.Cases = append(out.Cases, cr)
	}
	out.DurationMs = time.Since(start).Milliseconds()
	return out
}

// RunRegressionSuite executes the golden buyerflow regression (no Encore/Postgres).
func RunRegressionSuite() RegressionRunResult {
	start := time.Now()
	sim := NewOmahSimulator()
	suites := []RegressionSuiteResult{
		runRegressionCases("buyerflow_golden", regressionCases, sim),
		runScriptSuite("buyerflow_order_script", []struct {
			input    string
			wantPath string
		}{
			{"mau order abon sapi yang 500 gram 3 biji ya", PathOrderFlow},
			{"Nama: Suciati\nHP: 081222333090", PathOrderFlow},
		}),
		runScriptSuite("buyerflow_shipping_script", []struct {
			input    string
			wantPath string
		}{
			{"mau tanya produk dulu", PathCatalogDB},
			{"berapa ongkir ke bandung?", PathShippingFAQ},
			{"berapa lama pengiriman?", PathShippingFAQ},
		}),
		runOrderRevisionSuite(),
		runPaymentFAQSuite(),
		runOrderRefSuite(),
	}
	if autogen := runAutoGenRegressionSuite(); len(autogen.Cases) > 0 {
		suites = append(suites, autogen)
	}
	passed := true
	for _, s := range suites {
		if !s.Skipped && !s.Passed {
			passed = false
		}
	}
	return RegressionRunResult{
		Passed:     passed,
		DurationMs: time.Since(start).Milliseconds(),
		Suites:     suites,
	}
}

func runOrderRevisionSuite() RegressionSuiteResult {
	start := time.Now()
	out := RegressionSuiteResult{Name: "buyerflow_order_revision", Passed: true}
	sim := NewOmahSimulator()
	setup := sim.Turn("mau beli abon sapi 2 pcs")
	if setup.Path != PathOrderFlow {
		out.Passed = false
		out.Cases = append(out.Cases, RegressionCaseResult{
			Name: "setup_order", Passed: false,
			Error: fmt.Sprintf("setup order path=%q", setup.Path),
		})
		out.DurationMs = time.Since(start).Milliseconds()
		return out
	}
	rev := sim.Turn("revisi jadi 5 pcs")
	cr := RegressionCaseResult{Name: "revision_qty", Passed: true}
	if rev.Path != PathOrderFlow {
		cr.Passed = false
		cr.Error = fmt.Sprintf("revision path=%q want %q reply=%q", rev.Path, PathOrderFlow, rev.Reply)
		out.Passed = false
	}
	out.Cases = append(out.Cases, cr)
	out.DurationMs = time.Since(start).Milliseconds()
	return out
}

func runPaymentFAQSuite() RegressionSuiteResult {
	start := time.Now()
	out := RegressionSuiteResult{Name: "buyerflow_payment_faq", Passed: true}
	cat := "Nomor Rekening"
	kb := []KBEntry{{
		Question: "Nomor Rekening",
		Answer:   "BCA 110220330 atas nama Omah Apparel",
		Category: &cat,
		IsActive: true,
	}}
	ans, ok := TryPaymentFAQAnswer("bisa minta nomor rekeningnya ga sih ?", kb)
	if !ok {
		out.Passed = false
		out.Cases = append(out.Cases, RegressionCaseResult{Name: "payment_match", Passed: false, Error: "expected payment FAQ match"})
	} else if !strings.Contains(strings.ToUpper(ans), "BCA") {
		out.Passed = false
		out.Cases = append(out.Cases, RegressionCaseResult{Name: "payment_match", Passed: false, Error: fmt.Sprintf("unexpected answer: %q", ans)})
	} else {
		out.Cases = append(out.Cases, RegressionCaseResult{Name: "payment_match", Passed: true})
	}
	_, ok = TryPaymentFAQAnswer("best seller apa?", kb)
	if ok {
		out.Passed = false
		out.Cases = append(out.Cases, RegressionCaseResult{Name: "non_payment_reject", Passed: false, Error: "non-payment question should not match payment FAQ"})
	} else {
		out.Cases = append(out.Cases, RegressionCaseResult{Name: "non_payment_reject", Passed: true})
	}
	out.DurationMs = time.Since(start).Milliseconds()
	return out
}

func runOrderRefSuite() RegressionSuiteResult {
	start := time.Now()
	out := RegressionSuiteResult{Name: "buyerflow_order_ref", Passed: true}
	checks := []struct {
		name string
		text string
		want bool
	}{
		{"bare_ref", "WB-58D662BC", true},
		{"detail_ref", "mau lihat detail pesanan WB-58D662BC", true},
		{"greeting", "halo kak", false},
	}
	for _, c := range checks {
		got := IsOrderRefStatusLookup(c.text)
		cr := RegressionCaseResult{Name: c.name, Passed: got == c.want}
		if !cr.Passed {
			cr.Error = fmt.Sprintf("IsOrderRefStatusLookup(%q)=%v want %v", c.text, got, c.want)
			out.Passed = false
		}
		out.Cases = append(out.Cases, cr)
	}
	out.DurationMs = time.Since(start).Milliseconds()
	return out
}

func runAutoGenRegressionSuite() RegressionSuiteResult {
	cases := conversationRegressionAutoGenCases()
	if len(cases) == 0 {
		return RegressionSuiteResult{Name: "buyerflow_autogen", Skipped: true, SkipReason: "no auto-generated cases", Passed: true}
	}
	sim, err := simulatorFromTriageAutoGenSnapshot()
	if err != nil {
		return RegressionSuiteResult{
			Name: "buyerflow_autogen", Passed: false,
			Cases: []RegressionCaseResult{{Name: "snapshot", Passed: false, Error: err.Error()}},
		}
	}
	return runRegressionCases("buyerflow_autogen", cases, sim)
}
