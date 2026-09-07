package ai

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestParseOutboundPath(t *testing.T) {
	meta, _ := json.Marshal(AiReplyMeta{Path: PathPaymentFAQ, Reason: "ai_generated"})
	if got := ParseOutboundPath(meta); got != PathPaymentFAQ {
		t.Fatalf("path = %q want %q", got, PathPaymentFAQ)
	}
	if got := ParseOutboundPath(nil); got != "" {
		t.Fatalf("empty metadata should return empty path")
	}
}

func TestIsNonDeterministicTriagePath(t *testing.T) {
	if !IsNonDeterministicTriagePath(PathLLM) {
		t.Fatal("llm should be non-deterministic")
	}
	if IsNonDeterministicTriagePath(PathPaymentFAQ) {
		t.Fatal("payment_faq should be deterministic")
	}
}

func TestCompareConversationRoutes_mismatch(t *testing.T) {
	sim := newOmahSimulator()
	messages := []TriageMessage{
		{ID: "in-1", Direction: "in", Body: "bisa minta nomor rekeningnya ga sih ?"},
		{ID: "out-1", Direction: "out", Metadata: mustMetaPath(t, PathCatalogDB)},
	}
	result := CompareConversationRoutes(sim, messages, "")
	if !result.HasDeterministic {
		t.Fatal("expected deterministic mismatch")
	}
	if len(result.Mismatches) != 1 {
		t.Fatalf("mismatches len = %d want 1", len(result.Mismatches))
	}
	m := result.Mismatches[0]
	if m.ExpectedPath != PathPaymentFAQ {
		t.Fatalf("expectedPath = %q want %q", m.ExpectedPath, PathPaymentFAQ)
	}
	if m.ActualPath != PathCatalogDB {
		t.Fatalf("actualPath = %q want %q", m.ActualPath, PathCatalogDB)
	}
}

func TestCompareConversationRoutes_match(t *testing.T) {
	sim := newOmahSimulator()
	messages := []TriageMessage{
		{ID: "in-1", Direction: "in", Body: "bisa minta nomor rekeningnya ga sih ?"},
		{ID: "out-1", Direction: "out", Metadata: mustMetaPath(t, PathPaymentFAQ)},
	}
	result := CompareConversationRoutes(sim, messages, "")
	if result.HasDeterministic {
		t.Fatal("expected no mismatch")
	}
	if result.TurnsChecked != 1 {
		t.Fatalf("turnsChecked = %d want 1", result.TurnsChecked)
	}
}

func TestCompareConversationRoutes_skipLLM(t *testing.T) {
	sim := newOmahSimulator()
	messages := []TriageMessage{
		{ID: "in-1", Direction: "in", Body: "halo kak"},
		{ID: "out-1", Direction: "out", Metadata: mustMetaPath(t, PathLLM)},
	}
	result := CompareConversationRoutes(sim, messages, "")
	if result.TurnsChecked != 0 {
		t.Fatalf("turnsChecked = %d want 0", result.TurnsChecked)
	}
	if result.TurnsSkipped != 1 {
		t.Fatalf("turnsSkipped = %d want 1", result.TurnsSkipped)
	}
}

func TestCompareConversationRoutes_focusInbound(t *testing.T) {
	sim := newOmahSimulator()
	messages := []TriageMessage{
		{ID: "in-1", Direction: "in", Body: "best seller di toko ini apa ?"},
		{ID: "out-1", Direction: "out", Metadata: mustMetaPath(t, PathOrderStatus)},
		{ID: "in-2", Direction: "in", Body: "bisa minta nomor rekeningnya ga sih ?"},
		{ID: "out-2", Direction: "out", Metadata: mustMetaPath(t, PathCatalogDB)},
	}
	result := CompareConversationRoutes(sim, messages, "in-2")
	if len(result.Mismatches) != 1 {
		t.Fatalf("mismatches len = %d want 1", len(result.Mismatches))
	}
	if result.Mismatches[0].InboundID != "in-2" {
		t.Fatalf("inboundId = %q want in-2", result.Mismatches[0].InboundID)
	}
}

func TestGenerateRegressionCases(t *testing.T) {
	code := GenerateRegressionCases([]TriageMismatch{{
		InboundID:    "abc",
		UserText:     "bisa minta nomor rekeningnya ga sih ?",
		ExpectedPath: PathPaymentFAQ,
	}}, "t_omah_apparel", SimulatorToSnapshot(newOmahSimulator(), "t_omah_apparel"))
	if !strings.Contains(code, "wantPath: PathPaymentFAQ") {
		t.Fatalf("wantPath should reference const, got: %s", code)
	}
	if strings.Contains(code, `wantPath: "PathPaymentFAQ"`) {
		t.Fatalf("wantPath must not be quoted const name: %s", code)
	}
	if !strings.Contains(code, "nomor rekening") {
		t.Fatalf("missing input text: %s", code)
	}

	consulting := GenerateRegressionCases([]TriageMismatch{{
		InboundID:    "1df31813",
		UserText:     "berapa harga kaosnya?",
		ExpectedPath: PathConsulting,
	}}, "t_omah_apparel", SimulatorToSnapshot(newOmahSimulator(), "t_omah_apparel"))
	if !strings.Contains(consulting, "wantPath: PathConsulting") {
		t.Fatalf("consulting path should use const: %s", consulting)
	}
	if strings.Contains(consulting, `\"consulting\"`) {
		t.Fatalf("consulting path must not be double-quoted: %s", consulting)
	}

	multi := GenerateRegressionCases([]TriageMismatch{{
		InboundID:    "abc",
		UserText:     "mau order abon",
		ExpectedPath: PathOrderFlow,
		PriorTurns:   []string{"halo", "ada stok?"},
	}}, "t_omah_apparel", SimulatorToSnapshot(newOmahSimulator(), "t_omah_apparel"))
	if !strings.Contains(multi, `priorInputs: []string{"halo", "ada stok?"}`) {
		t.Fatalf("expected priorInputs in generated code: %s", multi)
	}
	if !strings.Contains(code, "triageAutoGenSnapshotJSON") {
		t.Fatalf("expected catalog snapshot in generated code: %s", code)
	}
}

func TestGenerateRegressionCases_newlineNotDoubleEscaped(t *testing.T) {
	code := GenerateRegressionCases([]TriageMismatch{{
		InboundID:    "nl",
		UserText:     "a\nb",
		ExpectedPath: PathOrderFlow,
	}}, "t_omah_apparel", nil)
	quoted := strconv.Quote("a\nb")
	if !strings.Contains(code, "input: "+quoted) {
		t.Fatalf("generated input should be Go-quoted newline, got:\n%s", code)
	}
	if strings.Contains(code, `input: "a\\nb"`) {
		t.Fatalf("newline was double-escaped:\n%s", code)
	}
}

func TestGenerateRegressionCases_skipsUntrustedConsultingOrderList(t *testing.T) {
	code := GenerateRegressionCases([]TriageMismatch{{
		InboundID:    "ord",
		UserText:     "abon sapi 125 gr 1\nabon sapi 250 gram 1",
		ExpectedPath: PathConsulting,
	}}, "t_omah_apparel", nil)
	if strings.Contains(code, "triage_ord") {
		t.Fatalf("order list must not emit consulting golden case:\n%s", code)
	}
}

func TestCountRegressionMismatches_skipsUntrusted(t *testing.T) {
	mismatches := []TriageMismatch{
		{InboundID: "a", UserText: "halo", ExpectedPath: PathGreeting},
		{InboundID: "b", UserText: "sku 1\nsku 2", ExpectedPath: PathConsulting},
	}
	if got := CountRegressionMismatches(mismatches); got != 1 {
		t.Fatalf("CountRegressionMismatches = %d want 1", got)
	}
}

func TestSuggestWantPath_orderListConsultingUntrusted(t *testing.T) {
	path, untrusted := SuggestWantPath(TriageMismatch{
		UserText:     "abon 1\nmaggi 2",
		ExpectedPath: PathConsulting,
	})
	if !untrusted || path != "" {
		t.Fatalf("want untrusted empty path, got path=%q untrusted=%v", path, untrusted)
	}
	path, untrusted = SuggestWantPath(TriageMismatch{
		UserText:     "berapa harga kaosnya?",
		ExpectedPath: PathConsulting,
	})
	if untrusted || path != PathConsulting {
		t.Fatalf("plain consulting should be trusted, got path=%q untrusted=%v", path, untrusted)
	}
}

func TestVerifyGoldenCases_allPassed(t *testing.T) {
	sim := newOmahSimulator()
	out := sim.Turn("halo")
	factory := func() *ConversationSimulator { return newOmahSimulator() }
	got := VerifyGoldenCases(factory, []TriageMismatch{{
		InboundID:    "ok",
		UserText:     "halo",
		ExpectedPath: out.Path,
	}})
	if !got.AllPassed {
		t.Fatalf("expected all passed, failures=%v checked=%d", got.Failures, got.TurnsChecked)
	}
}

func TestVerifyGoldenCases_pathMismatch(t *testing.T) {
	factory := func() *ConversationSimulator { return newOmahSimulator() }
	got := VerifyGoldenCases(factory, []TriageMismatch{{
		InboundID:    "bad",
		UserText:     "halo",
		ExpectedPath: PathCatalogDB,
	}})
	if got.AllPassed {
		t.Fatal("expected failure")
	}
	if len(got.Failures) != 1 {
		t.Fatalf("failures = %d want 1", len(got.Failures))
	}
	if got.Failures[0].WantPath != PathCatalogDB {
		t.Fatalf("wantPath = %q", got.Failures[0].WantPath)
	}
}

func TestVerifyGoldenCases_skipsUntrustedOrderList(t *testing.T) {
	factory := func() *ConversationSimulator { return newOmahSimulator() }
	got := VerifyGoldenCases(factory, []TriageMismatch{{
		InboundID:    "ord",
		UserText:     "abon 1\nmaggi 2",
		ExpectedPath: PathConsulting,
	}})
	if got.TurnsChecked != 0 {
		t.Fatalf("untrusted case should be skipped, checked=%d", got.TurnsChecked)
	}
	if got.AllPassed {
		t.Fatal("zero golden turns is not success")
	}
}

func TestCountRegressionMismatches(t *testing.T) {
	mismatches := []TriageMismatch{
		{InboundID: "a", UserText: "halo", ExpectedPath: PathGreeting},
		{InboundID: "b", UserText: "foto", Skipped: true, SkipReason: "media"},
		{InboundID: "c", UserText: "", ExpectedPath: PathOrderFlow},
	}
	if got := CountRegressionMismatches(mismatches); got != 1 {
		t.Fatalf("CountRegressionMismatches = %d want 1", got)
	}
}

func mustMetaPath(t *testing.T, path string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(AiReplyMeta{Path: path, Reason: "ai_generated"})
	if err != nil {
		t.Fatal(err)
	}
	return b
}
