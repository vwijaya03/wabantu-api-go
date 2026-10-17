package ai

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	bf "encore.app/wabantu/internal/buyerflow"
	"encore.app/wabantu/internal/salesbrain"
	appdb "encore.app/wabantu/shared/db"
)

func TestLastOutboundWasLanguageAsk(t *testing.T) {
	yes := map[string]string{
		"legacy ask reply":          bf.LanguagePreferenceAskReply(),
		"salesbrain choice (id)":    salesbrain.T(salesbrain.TplLanguageChoice, salesbrain.LangID, "Omah Apparel"),
		"salesbrain choice (en)":    salesbrain.T(salesbrain.TplLanguageChoice, salesbrain.LangEN, "Omah Apparel"),
		"marker only, other case":   "mau pakai BAHASA INDONESIA ATAU ENGLISH?",
		"english line of the ask":   "Hello! Would you like to continue in Bahasa Indonesia or English?",
		"embedded in a longer text": "Halo kak!\n" + bf.LanguagePreferenceAskReply(),
	}
	for name, body := range yes {
		if !lastOutboundWasLanguageAsk(body) {
			t.Errorf("%s: %q must be recognised as the language question", name, body)
		}
	}
	no := []string{
		"How many pcs? (e.g. 1 pc)",
		"Berapa pcs? (contoh: 1 pcs)",
		"Halo kak, ada yang bisa dibantu?",
		"Kami kirim pakai J&T atau JNE ya kak",
		"",
	}
	for _, body := range no {
		if lastOutboundWasLanguageAsk(body) {
			t.Errorf("%q is not the language question", body)
		}
	}
}

// bodyTS answers the "last outbound body" query.
type bodyTS struct {
	*recordingTS
	body    string
	err     error
	queries int
}

func (b *bodyTS) QueryRowContext(_ context.Context, q string, _ ...any) appdb.Scannable {
	b.queries++
	return rowFunc(func(dest ...any) error {
		if b.err != nil {
			return b.err
		}
		*(dest[0].(*string)) = b.body
		return nil
	})
}

func TestLastAIOutboundBody(t *testing.T) {
	got, err := lastAIOutboundBody(context.Background(), &bodyTS{recordingTS: &recordingTS{}, body: "hello"}, "c1")
	if err != nil || got != "hello" {
		t.Fatalf("got (%q, %v)", got, err)
	}
	got, err = lastAIOutboundBody(context.Background(), &bodyTS{recordingTS: &recordingTS{}, err: sql.ErrNoRows}, "c1")
	if err != nil || got != "" {
		t.Fatalf("no outbound message yet is not an error: got (%q, %v)", got, err)
	}
	if _, err = lastAIOutboundBody(context.Background(), &bodyTS{recordingTS: &recordingTS{}, err: errors.New("db down")}, "c1"); err == nil {
		t.Fatal("a real DB error must be returned")
	}
}

func TestLanguageQuestionAsked(t *testing.T) {
	ask := &bodyTS{recordingTS: &recordingTS{}, body: bf.LanguagePreferenceAskReply()}
	if !languageQuestionAsked(context.Background(), ask, "c1", "2") {
		t.Fatal(`"2" right after the language question is the answer`)
	}
	other := &bodyTS{recordingTS: &recordingTS{}, body: "How many pcs? (e.g. 1 pc)"}
	if languageQuestionAsked(context.Background(), other, "c1", "1") {
		t.Fatal(`"1" after the quantity question is a quantity, not a language answer`)
	}
	// Text that cannot be a language answer never costs a query.
	idle := &bodyTS{recordingTS: &recordingTS{}, body: bf.LanguagePreferenceAskReply()}
	if languageQuestionAsked(context.Background(), idle, "c1", "berapa harga tuna steak 100 gram?") || idle.queries != 0 {
		t.Fatalf("a non-answer text must short-circuit without a query (queries=%d)", idle.queries)
	}
	broken := &bodyTS{recordingTS: &recordingTS{}, err: errors.New("db down")}
	if languageQuestionAsked(context.Background(), broken, "c1", "1") {
		t.Fatal("when the last outbound cannot be read the answer is NOT taken as a language choice")
	}
}

func languageHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, nil)
	// handleBuyerLanguageRouting updates the per-turn saved language stack; keep it out of other tests.
	pop := bf.PushSavedBuyerLang("")
	t.Cleanup(pop)
	return h
}

func confirmText(lang string) string {
	return salesbrain.T(salesbrain.TplSwitchConfirm, salesbrain.ParseLang(lang))
}

// A quantity answer like "1" used to be read as a language choice and switched the buyer's language
// (S1). Only the answer to the language question counts as one.
func TestBareAnswerSwitchesLanguageOnlyWhenAsked(t *testing.T) {
	answerLang, ok := bf.ParseLanguagePreferenceAnswer("1")
	if !ok {
		t.Fatal(`fixture: "1" must look like a language answer to the legacy parser`)
	}
	convo := &dbConversation{ID: "c1", AIHandled: true}

	h := languageHarness(t)
	handled, err := (&AutoReplyService{}).handleBuyerLanguageRouting(context.Background(), h.ts, testPayload(), convo,
		simChannel(), contactWithLang("en"), "1", false)
	if err != nil || handled {
		t.Fatalf("not asked: handled=%v err=%v, want the turn to continue", handled, err)
	}
	if len(h.sent) != 0 || len(h.ts.execs) != 0 {
		t.Fatalf("a quantity must not send a confirmation or touch the contact: sent=%+v execs=%+v", h.sent, h.ts.execs)
	}

	h = languageHarness(t)
	contact := contactWithLang("")
	handled, err = (&AutoReplyService{}).handleBuyerLanguageRouting(context.Background(), h.ts, testPayload(), convo,
		simChannel(), contact, "1", true)
	if err != nil || !handled {
		t.Fatalf("asked: handled=%v err=%v", handled, err)
	}
	if len(h.sent) != 1 || h.sent[0].text != confirmText(answerLang) {
		t.Fatalf("sent = %+v, want the %q confirmation %q", h.sent, answerLang, confirmText(answerLang))
	}
	if len(h.ts.execs) != 1 || !strings.Contains(h.ts.execs[0].query, "preferred_language") {
		t.Fatalf("the choice must be saved on the contact: %+v", h.ts.execs)
	}
	if contactPreferredLanguage(contact) != answerLang {
		t.Fatalf("contact language = %q, want %q", contactPreferredLanguage(contact), answerLang)
	}
}

// An explicit request ("please reply in English") works at any time, asked or not, and is
// confirmed with the salesbrain template in the new language.
func TestExplicitLanguageSwitchAlwaysActive(t *testing.T) {
	h := languageHarness(t)
	handled, err := (&AutoReplyService{}).handleBuyerLanguageRouting(context.Background(), h.ts, testPayload(),
		&dbConversation{ID: "c1", AIHandled: true}, simChannel(), contactWithLang("id"), "please reply in English", false)
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if len(h.sent) != 1 || h.sent[0].text != salesbrain.T(salesbrain.TplSwitchConfirm, salesbrain.LangEN) {
		t.Fatalf("sent = %+v", h.sent)
	}
	if h.sent[0].text != "Sure, I'll reply in English from now on." {
		t.Fatalf("confirmation must be the salesbrain EN template, got %q", h.sent[0].text)
	}
	if len(h.ts.execs) != 1 || !strings.Contains(strings.Join(argsToStrings(h.ts.execs[0].args), ","), "en") {
		t.Fatalf("English must be saved: %+v", h.ts.execs)
	}
}

func argsToStrings(args []any) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if s, ok := a.(string); ok {
			out[i] = s
		}
	}
	return out
}

// An ambiguous greeting still gets the language question (unchanged).
func TestAmbiguousGreetingStillAsksLanguage(t *testing.T) {
	h := languageHarness(t)
	handled, err := (&AutoReplyService{}).handleBuyerLanguageRouting(context.Background(), h.ts, testPayload(),
		&dbConversation{ID: "c1", AIHandled: true}, simChannel(), contactWithLang(""), "hello", false)
	if err != nil || !handled || len(h.sent) != 1 || !lastOutboundWasLanguageAsk(h.sent[0].text) {
		t.Fatalf("handled=%v err=%v sent=%+v", handled, err, h.sent)
	}
}

// The deferred persist (applyBuyerLanguageFromMessage) must not lock a language from a bare
// "1"/"2" the bot never asked for either, while a clear message still locks.
func TestApplyBuyerLanguageIgnoresUnaskedBareAnswer(t *testing.T) {
	apply := func(saved, text string, asked bool) (*recordingTS, *dbContact) {
		pop := bf.PushSavedBuyerLang(saved)
		defer pop()
		ts := &recordingTS{rowsAffected: 1}
		c := contactWithLang(saved)
		(&AutoReplyService{}).applyBuyerLanguageFromMessage(context.Background(), ts, c, text, asked)
		return ts, c
	}
	if ts, c := apply("", "2", false); len(ts.execs) != 0 || contactPreferredLanguage(c) != "" {
		t.Fatalf(`unasked "2" with nothing saved must not lock a language: %+v`, ts.execs)
	}
	if ts, _ := apply("", "2", true); len(ts.execs) != 1 {
		t.Fatalf(`"2" answering the language question must lock: %+v`, ts.execs)
	}
	if ts, c := apply("", "Hello i want to ask about your tuna steak product", false); len(ts.execs) != 1 || contactPreferredLanguage(c) != "en" {
		t.Fatalf("a clear English message locks English even when nothing was asked: %+v", ts.execs)
	}
	if ts, _ := apply("id", "1", false); len(ts.execs) != 0 {
		t.Fatalf("a saved language is a lock: %+v", ts.execs)
	}
	if ts, _ := apply("", "please reply in Indonesian", false); len(ts.execs) != 1 {
		t.Fatalf("an explicit request locks at any time: %+v", ts.execs)
	}
}
