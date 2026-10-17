package salesbrain

import (
	"strings"
	"testing"
)

func TestEveryTemplateHasBothLanguages(t *testing.T) {
	if len(TemplateKeys()) < 14 {
		t.Fatalf("expected at least 14 templates, got %d", len(TemplateKeys()))
	}
	for _, k := range TemplateKeys() {
		for _, l := range []Lang{LangID, LangEN} {
			if strings.TrimSpace(templates[k][l]) == "" {
				t.Errorf("template %s has no %s text", k, l)
			}
		}
	}
}

func TestEnglishTemplatesAreNotIndonesian(t *testing.T) {
	for _, k := range TemplateKeys() {
		if k == TplLanguageChoice {
			continue // intentionally bilingual
		}
		en := strings.ToLower(templates[k][LangEN])
		for _, w := range []string{" kak", "kakak", " aku ", " ya ", "pesanan", "tolong", "sudah"} {
			if strings.Contains(" "+en+" ", w) {
				t.Errorf("EN template %s contains Indonesian %q: %q", k, w, en)
			}
		}
	}
}

func TestTFormatsArguments(t *testing.T) {
	got := T(TplOrderCancelled, LangEN, "WB-F4BFCE06")
	if got != "Done, order WB-F4BFCE06 is cancelled. Would you like to start a new one?" {
		t.Fatalf("got %q", got)
	}
	if T(TemplateKey("nope"), LangEN) != "" {
		t.Fatal("unknown key must return empty string")
	}
	if got := T(TplHandoffTakeover, LangEN, "Omah Apparel", ""); strings.HasSuffix(got, " ") {
		t.Fatalf("empty trailing argument must not leave a trailing space: %q", got)
	}
}

func TestOpeningHoursSentence(t *testing.T) {
	if OpeningHoursSentence("", LangEN) != "" {
		t.Fatal("no hours → no promise")
	}
	if got := OpeningHoursSentence("08:00 - 19:00 GMT +7", LangID); got != "Biasanya dibalas di jam buka (08:00 - 19:00 GMT +7)." {
		t.Fatalf("got %q", got)
	}
	if got := OpeningHoursSentence("08:00 - 19:00 GMT +7", LangEN); got != "They usually reply during opening hours (08:00 - 19:00 GMT +7)." {
		t.Fatalf("got %q", got)
	}
}
