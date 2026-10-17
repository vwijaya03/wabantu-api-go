package buyerflow

import (
	"strings"
	"sync"
	"testing"
)

func TestLanguagePreferenceAnswerMatchesPromptOrder(t *testing.T) {
	// LanguagePreferenceAskReply says "Reply *Indonesia* / *English* (or *1* / *2*)".
	if l, ok := ParseLanguagePreferenceAnswer("1"); !ok || l != LangID {
		t.Fatalf(`"1" = (%q,%v), want id`, l, ok)
	}
	if l, ok := ParseLanguagePreferenceAnswer("2"); !ok || l != LangEN {
		t.Fatalf(`"2" = (%q,%v), want en`, l, ok)
	}
}

func TestLanguagePreferenceAnswerIgnoresPlaceNames(t *testing.T) {
	for _, s := range []string{
		"Ana\n0872839472828\nPerak, surabaya, indonesia",
		"Villa kalijudan indah A9, jawa timur, surabaya, indonesia",
		"Do you ship to all of indonesia?",
		"Do you have Indonesian beef?",
		"Can you deliver to Bali, Indonesia?",
	} {
		if l, ok := ParseLanguagePreferenceAnswer(s); ok {
			t.Errorf("%q → %q, want no language answer", s, l)
		}
	}
}

func TestExplicitSwitchNegationIsClauseScoped(t *testing.T) {
	cases := map[string]string{
		"No, i can’t speak bahasa indonesia":                LangEN,
		"i can’t speak bahasa indonesia":                    LangEN,
		"I don’t understand bahasa indonesia":               LangEN,
		"no, bahasa indonesia aja":                          LangID,
		"tidak bisa bahasa inggris, pakai bahasa indonesia": LangID,
		"can i talk in english":                             LangEN,
	}
	for msg, want := range cases {
		got, ok := ParseExplicitLanguageSwitch(msg)
		if !ok || got != want {
			t.Errorf("ParseExplicitLanguageSwitch(%q) = (%q,%v), want %q", msg, got, ok, want)
		}
	}
}

func TestSavedLanguageIsALock(t *testing.T) {
	for _, msg := range []string{
		"Gak bisa baca apa\n\nVilla kalijudan indah A9, jawa timur, surabaya",
		"Saya mau tanya produk",
		"Cancel my order",
		"Name: Ana\nPhone: 0872839472828\nAddress: Perak, surabaya, indonesia",
	} {
		for _, saved := range []string{LangEN, LangID} {
			if got := ResolveBuyerLanguage(msg, saved); got != saved {
				t.Errorf("ResolveBuyerLanguage(%q, %s) = %s, want lock %s", msg, saved, got, saved)
			}
			if l, ok := LanguageToPersist(msg, saved); ok {
				t.Errorf("LanguageToPersist(%q, %s) = %s, want no change", msg, saved, l)
			}
		}
	}
	if l, ok := LanguageToPersist("please reply in Indonesian", LangEN); !ok || l != LangID {
		t.Fatalf("explicit switch must persist, got (%q,%v)", l, ok)
	}
	if l, ok := LanguageToPersist("Hello i want to ask about your tuna steak product", ""); !ok || l != LangEN {
		t.Fatalf("first clear message must lock, got (%q,%v)", l, ok)
	}
}

// The saved-language stack is process-global. Concurrent turns may interleave their push/pop
// pairs, but they must never race on the slice itself (run with -race).
func TestSavedBuyerLangStackIsDataRaceFree(t *testing.T) {
	savedBuyerLangMu.Lock()
	before := append([]string(nil), savedBuyerLangStack...)
	savedBuyerLangMu.Unlock()
	t.Cleanup(func() {
		savedBuyerLangMu.Lock()
		savedBuyerLangStack = before
		savedBuyerLangMu.Unlock()
	})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				pop := PushSavedBuyerLang(LangEN)
				SetSavedBuyerLang(LangID)
				_ = CurrentSavedBuyerLang()
				pop()
			}
		}()
	}
	wg.Wait()
}

func TestLanguagePreferenceAnswerAcceptsOnlyBareAnswers(t *testing.T) {
	for msg, want := range map[string]string{
		"english": LangEN, "English please": LangEN, "Indonesia": LangID, "indo aja": LangID,
		"bahasa inggris": LangEN, "bahasa indonesia": LangID, "yang english dong": LangEN,
	} {
		if l, ok := ParseLanguagePreferenceAnswer(msg); !ok || l != want {
			t.Errorf("ParseLanguagePreferenceAnswer(%q) = (%q,%v), want %q", msg, l, ok, want)
		}
	}
	// A short sentence merely written in a language, or naming a place, is not an answer to the
	// language question: it must be handled as a normal message (shipping, payment, handoff...).
	for _, msg := range []string{
		"talk to a human", "I want to pay", "how long is shipping", "ongkir ke surabaya berapa",
		"mau beli dong", "tolong kirim", "Surabaya, Indonesia", "ship to Indonesia?", "tuna 1 please",
	} {
		if l, ok := ParseLanguagePreferenceAnswer(msg); ok {
			t.Errorf("ParseLanguagePreferenceAnswer(%q) = %q, want no language answer", msg, l)
		}
	}
}

func TestLanguagePreferenceAnswerIgnoredOnceLanguageIsSaved(t *testing.T) {
	for _, saved := range []string{LangEN, LangID} {
		pop := PushSavedBuyerLang(saved)
		// The language question is only asked while nothing is saved, so digits and bare words are
		// not answers any more (a "2" may be a quantity or a variant pick).
		for _, msg := range []string{"1", "2", "en", "id", "I want to pay", "mau beli dong"} {
			if l, ok := ParseLanguagePreferenceAnswer(msg); ok {
				t.Errorf("saved=%s: ParseLanguagePreferenceAnswer(%q) = %q, want no answer", saved, msg, l)
			}
		}
		// An explicit request still switches a locked language.
		other := LangID
		if saved == LangID {
			other = LangEN
		}
		req := map[string]string{LangID: "please reply in Indonesian", LangEN: "please reply in English"}[other]
		if l, ok := ParseLanguagePreferenceAnswer(req); !ok || l != other {
			t.Errorf("saved=%s: explicit %q = (%q,%v), want %s", saved, req, l, ok, other)
		}
		pop()
	}
}

func TestSimulatorShortSentencesAndDigitsAreNotLanguageAnswers(t *testing.T) {
	// Unlocked buyer: a short English command must not be swallowed as an answer to the menu.
	sim := newOmahSimulator()
	if out := sim.Turn("talk to a human"); out.Path == PathBuyerLanguage {
		t.Fatalf("unlocked: %q was taken as a language answer: %q", "talk to a human", out.Reply)
	}
	// Locked buyer: a bare digit must not flip the saved language.
	sim = newOmahSimulator()
	sim.SavedBuyerLang = LangEN
	if out := sim.Turn("2"); out.Path == PathBuyerLanguage || sim.SavedBuyerLang != LangEN {
		t.Fatalf("locked en: %q changed language (path=%s saved=%s)", "2", out.Path, sim.SavedBuyerLang)
	}
	sim = newOmahSimulator()
	sim.SavedBuyerLang = LangID
	if out := sim.Turn("1"); out.Path == PathBuyerLanguage || sim.SavedBuyerLang != LangID {
		t.Fatalf("locked id: %q changed language (path=%s saved=%s)", "1", out.Path, sim.SavedBuyerLang)
	}
}

func TestLanguageMenuNumbersFollowThePrompt(t *testing.T) {
	// 1 = Indonesia, 2 = English, exactly as LanguagePreferenceAskReply prints them.
	cases := map[string]struct{ lang, confirm string }{
		"1": {LangID, "Bahasa Indonesia"},
		"2": {LangEN, "English"},
	}
	for digit, want := range cases {
		sim := newOmahSimulator()
		if out := sim.Turn("hi"); out.Path != PathBuyerLanguage {
			t.Fatalf("setup: expected language ask, got path=%s", out.Path)
		}
		out := sim.Turn(digit)
		if out.Path != PathBuyerLanguage || sim.SavedBuyerLang != want.lang {
			t.Fatalf("%q: path=%s saved=%q, want %s", digit, out.Path, sim.SavedBuyerLang, want.lang)
		}
		if !strings.Contains(out.Reply, want.confirm) {
			t.Errorf("%q: confirm %q must mention %q", digit, out.Reply, want.confirm)
		}
	}
}
