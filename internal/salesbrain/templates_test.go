package salesbrain

import (
	"strings"
	"testing"
)

func TestEveryTemplateHasBothLanguages(t *testing.T) {
	if len(TemplateKeys()) < 15 {
		t.Fatalf("expected at least 15 templates, got %d", len(TemplateKeys()))
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

// TplCheckWithTeam replaces the old hard-coded Indonesian "Saya belum menemukan data..." that
// grounding used to send to every buyer whatever their language.
func TestCheckWithTeamTemplate(t *testing.T) {
	if got := T(TplCheckWithTeam, LangEN); got != "Let me double-check that with the shop team and get back to you here." {
		t.Fatalf("EN = %q", got)
	}
	if got := T(TplCheckWithTeam, LangID); got != "Sebentar ya Kak, aku cek dulu ke tim toko dan kabari di chat ini." {
		t.Fatalf("ID = %q", got)
	}
}

// The LLM-first shell never writes buyer-facing wording itself: the order number line, the
// sentence that names invalid checkout data and the "ship to" block of the final summary all come
// from templates so an English buyer never receives Indonesian labels.
func TestOrderRefProblemsAndShippingTemplates(t *testing.T) {
	cases := []struct {
		key  TemplateKey
		lang Lang
		args []any
		want string
	}{
		{TplOrderRef, LangEN, []any{"WB-F4BFCE06"}, "Order number: WB-F4BFCE06"},
		{TplOrderRef, LangID, []any{"WB-F4BFCE06"}, "Nomor pesanan: WB-F4BFCE06"},
		{TplFieldProblems, LangEN, []any{"the postal code 123 has 3 digits but should have 5"}, "Sorry, the postal code 123 has 3 digits but should have 5."},
		{TplFieldProblems, LangID, []any{"kode pos 123 belum valid (3 digit; seharusnya 5 digit)"}, "Maaf Kak, kode pos 123 belum valid (3 digit; seharusnya 5 digit)."},
		{TplShippingSummary, LangEN, []any{"Ana", "+62872839472828", "Villa Kalijudan Indah A9, Surabaya"}, "Shipping to:\nAna (+62872839472828)\nVilla Kalijudan Indah A9, Surabaya"},
		{TplShippingSummary, LangID, []any{"Ana", "+62872839472828", "Villa Kalijudan Indah A9, Surabaya"}, "Dikirim ke:\nAna (+62872839472828)\nVilla Kalijudan Indah A9, Surabaya"},
	}
	for _, c := range cases {
		if got := T(c.key, c.lang, c.args...); got != c.want {
			t.Errorf("T(%s, %s) = %q, want %q", c.key, c.lang, got, c.want)
		}
	}
}
