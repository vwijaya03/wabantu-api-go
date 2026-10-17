package salesbrain

import "testing"

func TestParseExplicitSwitch(t *testing.T) {
	cases := []struct {
		msg  string
		want Lang
		ok   bool
	}{
		// explicit requests
		{"English please", LangEN, true},
		{"can i talk in english", LangEN, true},
		{"Can you speak English?", LangEN, true},
		{"please reply in English", LangEN, true},
		{"switch to english", LangEN, true},
		{"bisa english?", LangEN, true},
		{"pakai bahasa inggris aja", LangEN, true},
		{"english", LangEN, true},
		{"Please reply in Indonesian", LangID, true},
		{"can we continue in bahasa indonesia?", LangID, true},
		{"ganti ke bahasa Indonesia", LangID, true},
		{"balas pakai bahasa indonesia dong", LangID, true},
		{"pake indo aja", LangID, true},
		{"no, bahasa indonesia aja", LangID, true},
		// inability = the other language
		{"No, i can’t speak bahasa indonesia", LangEN, true},
		{"i cant speak bahasa indonesia, why you suddenly changed the language ?", LangEN, true},
		{"I don’t understand bahasa indonesia", LangEN, true},
		{"aku nggak ngerti bahasa inggris", LangID, true},
		{"tidak bisa bahasa inggris, pakai bahasa indonesia", LangID, true},
		{"I don't understand English", LangID, true},
		// never a switch
		{"Do you ship to all of indonesia?", "", false},
		{"Can you deliver to Bali, Indonesia?", "", false},
		{"Do you have Indonesian beef?", "", false},
		{"Can you ship Indonesian beef to Jakarta?", "", false},
		{"Name: Ana\nPhone: 0872839472828\nAddress: Perak, surabaya, indonesia", "", false},
		{"Villa kalijudan indah A9, jawa timur, surabaya, indonesia, 60177", "", false},
		{"Gak bisa baca apa", "", false},
		{"pakai bahasa aja", "", false},
		{"2", "", false},
		{"Helllo", "", false},
		{"bisa kirim ke indonesia timur?", "", false},
	}
	for _, c := range cases {
		got, ok := ParseExplicitSwitch(c.msg)
		if got != c.want || ok != c.ok {
			t.Errorf("ParseExplicitSwitch(%q) = (%q,%v), want (%q,%v)", c.msg, got, ok, c.want, c.ok)
		}
	}
}

func TestClearLanguage(t *testing.T) {
	cases := []struct {
		msg  string
		want Lang
		ok   bool
	}{
		{"Hello i want to ask about your tuna steak product", LangEN, true},
		{"Halo kak\nI want to ask about the tuna steak product. How much weight for the tuna?", LangEN, true},
		{"Okay, can i order 1 tuna steak and 1 australian beef 500gr please?", LangEN, true},
		{"saya mau yellow tail tuna 1 dong", LangID, true},
		{"disini jual apa saja ya ?", LangID, true},
		{"Okee\nKalau begitu apakah salmon strak dan tunanya itu di potong fresh atau frozen", LangID, true},
		{"kak ready tuna steak nya?", LangID, true},
		{"Helllo", "", false},
		{"tuna", "", false},
		{"1 pc", "", false},
		{"Name: Anna\nPhone: 089272729284748", "", false},
	}
	for _, c := range cases {
		got, ok := ClearLanguage(c.msg)
		if got != c.want || ok != c.ok {
			t.Errorf("ClearLanguage(%q) = (%q,%v), want (%q,%v)", c.msg, got, ok, c.want, c.ok)
		}
	}
}

func TestDecideLanguageLockedNeverFlipsWithoutRequest(t *testing.T) {
	never := []string{
		"Gak bisa baca apa\n\nVilla kalijudan indah A9, jawa timur, surabaya",
		"Name: Ana\nPhone: 0872839472828\nAddress: Perak, surabaya, indonesia",
		"Saya mau tanya produk",
		"Cancel my order",
		"Bodo ya kamu",
		"delivered today or tomorrow woi",
		"2",
		"1",
		"Do you ship to all of indonesia?",
	}
	for _, msg := range never {
		for _, locked := range []string{"en", "id"} {
			d := DecideLanguage(locked, LangID, msg, false)
			if string(d.Lang) != locked || d.Persist != "" || d.Switched || d.AskChoice {
				t.Errorf("locked=%s msg=%q: got %+v, want stay %s with no persist", locked, msg, d, locked)
			}
		}
	}
}

func TestDecideLanguageExplicitSwitchWhileLocked(t *testing.T) {
	d := DecideLanguage("id", LangID, "No, i can’t speak bahasa indonesia", false)
	if d.Lang != LangEN || d.Persist != LangEN || !d.Switched {
		t.Fatalf("got %+v, want switch to en", d)
	}
	d = DecideLanguage("en", LangID, "tolong pakai bahasa indonesia saja", false)
	if d.Lang != LangID || d.Persist != LangID || !d.Switched {
		t.Fatalf("got %+v, want switch to id", d)
	}
	d = DecideLanguage("en", LangID, "please reply in English", false)
	if d.Lang != LangEN || d.Persist != "" || d.Switched {
		t.Fatalf("same language request must be a no-op, got %+v", d)
	}
}

func TestDecideLanguageUnlocked(t *testing.T) {
	cases := []struct {
		msg      string
		awaiting bool
		want     LanguageDecision
	}{
		{"Hello", false, LanguageDecision{Lang: LangID, AskChoice: true}},
		{"Helllo", false, LanguageDecision{Lang: LangID, AskChoice: true}},
		{"hi", false, LanguageDecision{Lang: LangID, AskChoice: true}},
		{"selamat sore", false, LanguageDecision{Lang: LangID}},
		{"halo kak", false, LanguageDecision{Lang: LangID}},
		{"Hello Good evening", false, LanguageDecision{Lang: LangEN}},
		{"good morning", false, LanguageDecision{Lang: LangEN}},
		{"Hello i want to ask about your tuna steak product", false, LanguageDecision{Lang: LangEN, Persist: LangEN}},
		{"saya mau yellow tail tuna 1 dong", false, LanguageDecision{Lang: LangID, Persist: LangID}},
		{"english", true, LanguageDecision{Lang: LangEN, Persist: LangEN, Switched: true}},
		{"Indonesia", true, LanguageDecision{Lang: LangID, Persist: LangID, Switched: true}},
		{"indo aja", true, LanguageDecision{Lang: LangID, Persist: LangID, Switched: true}},
		{"bahasa inggris", true, LanguageDecision{Lang: LangEN, Persist: LangEN, Switched: true}},
		{"bahasa indonesia", true, LanguageDecision{Lang: LangID, Persist: LangID, Switched: true}},
		{"tuna?", false, LanguageDecision{Lang: LangID}},
		{"can i talk in english", false, LanguageDecision{Lang: LangEN, Persist: LangEN, Switched: true}},
	}
	for _, c := range cases {
		got := DecideLanguage("", LangID, c.msg, c.awaiting)
		if got != c.want {
			t.Errorf("DecideLanguage(unlocked, %q, awaiting=%v) = %+v, want %+v", c.msg, c.awaiting, got, c.want)
		}
	}
}

func TestDecideLanguageEnglishTenantDefault(t *testing.T) {
	got := DecideLanguage("", LangEN, "tuna?", false)
	if got.Lang != LangEN || got.Persist != "" {
		t.Fatalf("got %+v, want soft en", got)
	}
}

func TestParseChoiceAnswer(t *testing.T) {
	for msg, want := range map[string]Lang{"english": LangEN, "Indonesia": LangID, "indo aja": LangID, "bahasa inggris": LangEN} {
		if got, ok := ParseChoiceAnswer(msg); !ok || got != want {
			t.Errorf("ParseChoiceAnswer(%q) = (%q,%v), want %q", msg, got, ok, want)
		}
	}
	for _, msg := range []string{"Name: Ana\nPhone: 0872839472828", "Villa kalijudan indah A9, jawa timur, surabaya, indonesia, 60177"} {
		if got, ok := ParseChoiceAnswer(msg); ok {
			t.Errorf("ParseChoiceAnswer(%q) = %q, want none", msg, got)
		}
	}
}
