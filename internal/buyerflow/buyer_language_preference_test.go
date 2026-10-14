package buyerflow

import (
	"strings"
	"testing"
)

func TestResolveBuyerLanguageOrder(t *testing.T) {
	if got := ResolveBuyerLanguage("please use english", ""); got != LangEN {
		t.Fatalf("switch: got %q", got)
	}
	if got := ResolveBuyerLanguage("1 pcs", LangEN); got != LangEN {
		t.Fatalf("saved en for short qty: got %q", got)
	}
	if got := ResolveBuyerLanguage("1 pcs", ""); got != LangID {
		t.Fatalf("default id for ambiguous short: got %q", got)
	}
}

func TestBareHiAsksLanguageWithoutSaving(t *testing.T) {
	sim := newOmahSimulator()
	out := sim.Turn("hi")
	if out.Path != PathBuyerLanguage {
		t.Fatalf("path=%s want %s reply=%q", out.Path, PathBuyerLanguage, out.Reply)
	}
	if sim.SavedBuyerLang != "" {
		t.Fatalf("preference should stay empty until answer, got %q", sim.SavedBuyerLang)
	}
	if !strings.Contains(out.Reply, "English") || !strings.Contains(out.Reply, "Indonesia") {
		t.Fatalf("expected bilingual ask: %q", out.Reply)
	}
}

func TestHiWantToShopEnglishNoLanguageMenu(t *testing.T) {
	sim := newOmahSimulator()
	out := sim.Turn("hi, I want to shop")
	if out.Path == PathBuyerLanguage {
		t.Fatalf("shopping greeting must not show language menu: %q", out.Reply)
	}
	if sim.SavedBuyerLang != LangEN {
		t.Fatalf("saved lang=%q want en", sim.SavedBuyerLang)
	}
}

func TestSavedEnglishOrderTemplateQtyPrompt(t *testing.T) {
	pop := PushSavedBuyerLang(LangEN)
	defer pop()
	tmpl := defaultOrderTemplatesForBuyer(false, "1 pcs")
	if !strings.Contains(tmpl.ClarifyQty, "How many") {
		t.Fatalf("expected English qty template: %q", tmpl.ClarifyQty)
	}
}

func TestExplicitSwitchToIndonesian(t *testing.T) {
	sim := newOmahSimulator()
	sim.SavedBuyerLang = LangEN
	out := sim.Turn("pakai bahasa indonesia")
	if out.Path != PathBuyerLanguage {
		t.Fatalf("path=%s reply=%q", out.Path, out.Reply)
	}
	if sim.SavedBuyerLang != LangID {
		t.Fatalf("saved=%q want id", sim.SavedBuyerLang)
	}
	if !strings.Contains(out.Reply, "Bahasa Indonesia") {
		t.Fatalf("confirm in Indonesian: %q", out.Reply)
	}
}

func TestRecipientPolicyStaysIndonesian(t *testing.T) {
	sim := newOmahSimulator()
	out := sim.Turn("mau beli atas nama orang lain")
	if out.Path != PathRecipientPolicy {
		t.Fatalf("path=%s want %s reply=%q", out.Path, PathRecipientPolicy, out.Reply)
	}
	if strings.Contains(strings.ToLower(out.Reply), "recipient") && !strings.Contains(out.Reply, "penerima") {
		t.Fatalf("expected Indonesian recipient policy: %q", out.Reply)
	}
}

func TestProductNameNotEnglishQuestion(t *testing.T) {
	if IsQuestionLike("maggi tandoori 1 pcs") {
		t.Fatal("product order line must not be English question")
	}
	if IsEnglishBuyerMessage("tandoori") {
		t.Fatal("single product token must not force English")
	}
}
