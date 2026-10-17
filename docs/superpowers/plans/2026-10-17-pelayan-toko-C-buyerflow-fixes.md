# Pelayan Toko — Part C: legacy fixes in `internal/buyerflow` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the incident bugs that live in the pure-Go legacy flow `internal/buyerflow`. The legacy path stays live for every tenant without the `ai_llm_first` flag, and it is the fail-open fallback of the new path.

**Architecture:** Small, test-first edits to existing functions. The language policy is unified with the new path: `buyerflow` delegates explicit-switch, choice-answer and lock decisions to `internal/salesbrain`, which has stdlib-only imports, so there is no import cycle. No new behavior beyond the spec. Existing tests that encode the buggy behavior are updated and listed in the commit message.

**Tech Stack:** Go 1.26, `go test ./internal/buyerflow/ -count=1` (must stay < 10 s).

Spec: `docs/superpowers/specs/2026-10-17-pelayan-toko-llm-first-design.md` §4.1, §5. API map of buyerflow: section "buyerflow API map" in the orchestrator notes; key facts are repeated in each task.

## Global Constraints

- Branch/worktree: Part C is split in two halves that run in parallel, each in its own worktree branched from `feat/pelayan-toko-llm-first` (HEAD contains `internal/salesbrain`):
  - **C-lang** (Tasks 1–4): worktree `/Users/viko/Documents/WABantu/.worktrees/api-go-pt-c-lang`, branch `feat/pt-c-lang`
  - **C-order** (Tasks 5–9): worktree `/Users/viko/Documents/WABantu/.worktrees/api-go-pt-c-order`, branch `feat/pt-c-order`
  - **File ownership:**
    - C-lang owns `buyer_locale.go`, `payment_reply.go`, `shipping_reply.go`, `shipping_checkout.go`, `human_handoff.go`, `greeting.go`, `internal/salesbrain/language.go`.
    - C-order owns `order_catalog.go`, `order_parse.go`, `order_inline_multi.go`, `order_draft_mutate.go`, `order_intent.go`, `order_line_correction.go`, `order_qty.go`, `sales_format.go`, `order_fsm.go`, `text_util.go`, `types.go`.
    - Neither half edits the other's files.
    - Test files may be touched by either, but only to fix expectations broken by its own change.
- Do not edit anything under `ai/`. That is Part D.
- Languages: `LangID = "id"`, `LangEN = "en"` are untyped string constants in buyerflow (`buyer_locale.go:9`). `salesbrain.Lang` is a distinct string type, so convert with `string(l)` and `salesbrain.ParseLang(s)`.
- History `[]Message` is oldest first. `Message.Author` ∈ `contact|ai|human|system`, `Direction` ∈ `in|out`.
- Fixtures for the incident tenant:
  - `omahSteakProfile()`, `omahSteakCatalog()` with IDs `beef-500`, `tbone-1500`, `salmon-100`, `salmon-250`, `tuna-100`;
  - `omahSteakKB()` with IDs `kb-eta`, `kb-full-pay`, `kb-min-1`, `kb-pay-method`, `kb-courier`, `kb-same-day`, `kb-same-day-2`, `kb-retur`, `kb-min-2`;
  - `newOmahSteakSimulator()`;
  - `Simulator.Turn(text) TurnOutcome` with fields `Path, Reply, Order, Completed, Canceled, BrokeFlow`; set `s.SavedBuyerLang = LangEN` for an English buyer.
- When an existing test asserts buggy behavior, update the expectation, and in the task commit message add the line `Tes diperbarui: <file>:<TestName> (<why>)`. Never delete a test to make the suite pass. Never touch the generated `regression_autogen_*` or `regression_behavior_*` files. If one of them fails, stop and report it.
- Commit messages are in Indonesian with a conventional prefix and end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Run the full `go test ./internal/buyerflow/ ./internal/salesbrain/ -count=1` before every commit. Use `set -o pipefail` when piping test output.
- Never print secrets or touch `.env*` files. Do not push.

---

## C-lang half

### Task 1: Unified language lock in buyerflow (B3)

**Files:**
- Modify: `internal/salesbrain/language.go` (export `ParseChoiceAnswer`)
- Modify: `internal/buyerflow/buyer_locale.go` (`ParseExplicitLanguageSwitch` :160, `ParseLanguagePreferenceAnswer` :216, `ResolveBuyerLanguage` :242, `LanguageToPersist` :259, stack funcs :57-84)
- Test: `internal/buyerflow/buyer_locale_lock_test.go` (new)

**Interfaces:**
- Consumes: `salesbrain.ParseExplicitSwitch(msg string) (salesbrain.Lang, bool)`, `salesbrain.ClearLanguage(msg) (Lang, bool)`, `salesbrain.ParseLang(s) Lang`
- Produces: `salesbrain.ParseChoiceAnswer(msg string) (Lang, bool)` (exported wrapper of `parseChoiceAnswer`); unchanged buyerflow signatures.

- [ ] **Step 1: Write the failing test** `internal/buyerflow/buyer_locale_lock_test.go`:
```go
package buyerflow

import "testing"

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
```

- [ ] **Step 2: Run it**

Run: `go test ./internal/buyerflow/ -run 'LanguagePreference|ExplicitSwitchNegation|SavedLanguageIsALock' -count=1`
Expected: FAIL. For example, `"1"` currently returns `en`, and the place-name cases currently return `id`.

- [ ] **Step 3: Export the choice parser in salesbrain** by appending to `internal/salesbrain/language.go`:
```go
// ParseChoiceAnswer parses a reply to the bilingual language question ("english", "indo aja",
// or a clear sentence). Numbers are not accepted here; callers that showed numbered options map
// them themselves.
func ParseChoiceAnswer(msg string) (Lang, bool) {
	if IsStructuredData(msg) {
		return "", false
	}
	return parseChoiceAnswer(msg)
}
```
Also add the following to `internal/salesbrain/language_test.go`. Expected: PASS. Note that a full address line is ≥4 tokens and has no function-word majority, so `ClearLanguage` returns `("", false)`.
```go
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
```

- [ ] **Step 4: Rewrite the four buyerflow functions in `buyer_locale.go`.** Keep their signatures. Add the import `"encore.app/wabantu/internal/salesbrain"`.
```go
// ParseExplicitLanguageSwitch — shared policy with the LLM-first path (salesbrain): a language
// name only counts with a request verb/tail or an inability statement in the same clause.
func ParseExplicitLanguageSwitch(userText string) (lang string, ok bool) {
	l, ok := salesbrain.ParseExplicitSwitch(userText)
	return string(l), ok
}

// ParseLanguagePreferenceAnswer parses a reply to LanguagePreferenceAskReply. Numbers follow the
// order shown in that prompt: 1 = Indonesia, 2 = English.
func ParseLanguagePreferenceAnswer(userText string) (lang string, ok bool) {
	switch strings.TrimSpace(userText) {
	case "1":
		return LangID, true
	case "2":
		return LangEN, true
	}
	if l, ok := salesbrain.ParseExplicitSwitch(userText); ok {
		return string(l), true
	}
	toks := strings.Fields(userText)
	if len(toks) > 4 {
		return "", false // long messages are not answers to the language question
	}
	l, ok := salesbrain.ParseChoiceAnswer(userText)
	return string(l), ok
}

// ResolveBuyerLanguage: explicit switch → saved preference (lock) → clear message language →
// per-turn saved stack → Indonesian.
func ResolveBuyerLanguage(userText, savedPref string) string {
	if lang, ok := ParseExplicitLanguageSwitch(userText); ok {
		return lang
	}
	if saved := normalizeLang(savedPref); saved == LangEN || saved == LangID {
		return saved
	}
	if lang, ok := ClearMessageLanguage(userText); ok {
		return lang
	}
	if saved := CurrentSavedBuyerLang(); saved == LangEN || saved == LangID {
		return saved
	}
	return LangID
}

// LanguageToPersist: an explicit switch always persists; otherwise only the first clear signal
// locks the language (a saved preference is never overwritten by auto-detection).
func LanguageToPersist(userText, savedPref string) (lang string, ok bool) {
	saved := normalizeLang(savedPref)
	if l, ok := ParseExplicitLanguageSwitch(userText); ok {
		if l == saved {
			return "", false
		}
		return l, true
	}
	if saved == LangEN || saved == LangID {
		return "", false
	}
	if ans, ok := ParseLanguagePreferenceAnswer(userText); ok {
		return ans, true
	}
	if l, ok := salesbrain.ClearLanguage(userText); ok {
		return string(l), true
	}
	return "", false
}
```
Change only `savedPref` handling inside `ShouldAskBuyerLanguagePreference` if a test requires it.

- [ ] **Step 5: Make the global stack data-race free.** Behaviour is unchanged. Add `var savedBuyerLangMu sync.Mutex` and lock it inside `PushSavedBuyerLang`, inside the pop closure it returns, and inside `SetSavedBuyerLang` and `CurrentSavedBuyerLang`. Add the `sync` import. Run `go test ./internal/buyerflow/ -race -run 'Language|Locale' -count=1` and expect PASS.

- [ ] **Step 6: Run the whole package**

Run: `set -o pipefail; go test ./internal/buyerflow/ ./internal/salesbrain/ -count=1 2>&1 | tail -30`
Expected: PASS, apart from tests that assert the old buggy behavior. Known candidates are in `buyer_language_preference_test.go`, `order_flow_locale_test.go` and `bilingual_*_test.go`.
- Inspect each failure. Update the expectation only when the old expectation contradicts spec §4.1, for example `"1"` → en, an address flipping the language, or a clear message overriding a saved preference.
- Record each updated test in the commit message.
- If the failure is anything else, fix the code instead.

- [ ] **Step 7: Commit**
```bash
git add internal/salesbrain/language.go internal/salesbrain/language_test.go internal/buyerflow/
git commit -m "fix(buyerflow): kunci bahasa memakai kebijakan salesbrain

Opsi 1 = Indonesia, 2 = English sesuai teks prompt; negasi per klausa dan
apostrof tipografis; nama tempat tidak mengganti bahasa; bahasa tersimpan
tidak ditimpa deteksi otomatis; stack bahasa dilindungi mutex.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Payment and shipping FAQ never return an empty answer (B7, S14, S28)

**Files:**
- Modify: `internal/buyerflow/payment_reply.go` (`tryPaymentFAQAnswer` :70, return at :103-110)
- Modify: `internal/buyerflow/shipping_reply.go` (`IsShippingFAQQuestion` :6 phrase list :24, `tryShippingKBFAQ` localized return :86)
- Modify: `internal/buyerflow/shipping_checkout.go` (:66-72, use of the localized KB answer)
- Test: `internal/buyerflow/faq_localized_test.go` (new)

**Interfaces:**
- Produces: unchanged signatures. The contract changes: `TryPaymentFAQAnswer(q, kb)` never returns `("", true)`. For "where/which account/how to pay" questions it prefers the KB entry that contains an account number.

- [ ] **Step 1: Write the failing test** `internal/buyerflow/faq_localized_test.go`:
```go
package buyerflow

import (
	"strings"
	"testing"
)

func steakKBWithAccounts() []KBEntry {
	kb := omahSteakKB()
	pay := "pembayaran"
	return append(kb, KBEntry{ID: "kb-pay-accounts", Question: "Apa saja metode pembayaran yang tersedia ?", Answer: "Saat ini kami menerima pembayaran melalui Transfer Bank dan QRIS.\n\nBank Transfer:\nBCA - 000111222 - Omah Apparel\nMandiri - 333111222 - Omah Apparel", Category: &pay, IsActive: true})
}

func TestPaymentFAQNeverReturnsEmptyOK(t *testing.T) {
	pop := PushSavedBuyerLang(LangEN)
	defer pop()
	for _, q := range []string{"Where should i send payment?", "How do I pay?", "Can I pay with QRIS?", "how do i pay for my order"} {
		ans, ok := TryPaymentFAQAnswer(q, steakKBWithAccounts())
		if ok && strings.TrimSpace(ans) == "" {
			t.Errorf("TryPaymentFAQAnswer(%q) = (\"\", true): empty answers must fall through", q)
		}
	}
}

func TestPaymentFAQPrefersAccountEntry(t *testing.T) {
	pop := PushSavedBuyerLang(LangID)
	defer pop()
	ans, ok := TryPaymentFAQAnswer("Mau transfer ke rekening mana kak?", steakKBWithAccounts())
	if !ok || !strings.Contains(ans, "000111222") {
		t.Fatalf("got (%q,%v), want the entry with account numbers", ans, ok)
	}
}

func TestShippingFAQRecognisesDeliveredTodayQuestions(t *testing.T) {
	for _, q := range []string{"delivered today or tomorrow woi", "Is it going to be delivered today or tomorrow?", "can it arrive today?", "sampai hari ini atau besok?"} {
		if !IsShippingFAQQuestion(q) {
			t.Errorf("IsShippingFAQQuestion(%q) = false, want true", q)
		}
	}
}

func TestShippingKBNeverEmptyForEnglish(t *testing.T) {
	pop := PushSavedBuyerLang(LangEN)
	defer pop()
	reply, ok := TryShippingFAQReply("can you do same day delivery?", omahSteakProfile(), omahSteakKB(), false)
	if !ok || strings.TrimSpace(reply) == "" {
		t.Fatalf("got (%q,%v)", reply, ok)
	}
	if !strings.Contains(reply, "08:00") {
		t.Errorf("same-day question must mention the 08:00–12:00 dispatch rule, got %q", reply)
	}
}
```

- [ ] **Step 2: Run it**

Run: `go test ./internal/buyerflow/ -run 'PaymentFAQ|ShippingFAQRecognises|ShippingKBNeverEmpty' -count=1`
Expected: FAIL. Payment returns an empty OK answer. "delivered today" is not recognised. The same-day question answers "3-4 days".

- [ ] **Step 3: Implement**
- `payment_reply.go`, final return:
```go
	if bestScore > 0 || hasFallback {
		ans := strings.TrimSpace(pick.Answer)
		if ans != "" {
			if localized := strings.TrimSpace(LocalizeKBAnswerForBuyer(ans, query)); localized != "" {
				return localized, true
			}
		}
	}
	return "", false
```
- In the same function's scoring loop, add a bonus of `+0.5` for entries whose answer contains a run of ≥6 digits. Add it only when `asksWhereToPay(query)` is true, where `asksWhereToPay` matches: rekening, transfer ke, bayar ke mana, kemana, where, which account, account number, send payment, how do i pay, how to pay. Implement it as a small helper with a `[]string` of substrings over the lowercased query.
- `shipping_reply.go` `IsShippingFAQQuestion`: add these phrases to the list: `"delivered today"`, `"delivered tomorrow"`, `"arrive today"`, `"arrive tomorrow"`, `"today or tomorrow"`, `"hari ini atau besok"`, `"sampai hari ini"`, `"sampai besok"`, `"same-day"`.
- `shipping_reply.go` `tryShippingKBFAQ`: treat an empty `LocalizeKBAnswerForBuyer` result as "no KB answer", and return `("", false)` so the caller uses `BuildShippingFAQReply`.
  - When `asksSameDayDelivery(userText)` is true, pick the same-day KB fact before the ETA fact. Use `extractShippingKBFacts`; same-day uses `sameDayEN`/`sameDayID`.
  - Then append the ETA fact when present, so that "same day?" answers "dispatched the same day if paid 08:00–12:00 WIB; delivery estimated 3–4 days".
- `shipping_checkout.go`: where the KB shipping answer becomes `base`, guard against `""` the same way.

- [ ] **Step 4: Run** `set -o pipefail; go test ./internal/buyerflow/ -count=1 2>&1 | tail -30`. Expected: PASS. Apply the Task 1 Step 6 rule for old-behavior tests.

- [ ] **Step 5: Commit** with the message `fix(buyerflow): FAQ bayar/kirim tidak pernah kosong dan utamakan rekening`, the `Tes diperbarui:` lines if any, and the Co-Authored-By trailer.

---

### Task 3: Human escalation phrases (O3, S19)

**Files:**
- Modify: `internal/buyerflow/human_handoff.go` (regexes :25-26 and the explicit list :8)
- Test: `internal/buyerflow/human_handoff_phrases_test.go` (new)

- [ ] **Step 1: Failing test**
```go
package buyerflow

import "testing"

func TestHumanEscalationPhrasesEN(t *testing.T) {
	for _, s := range []string{"Contact your administrator", "can i speak with the owner", "i want to talk to a person", "let me speak to the owner", "can i talk to a real person", "I want to complain to the manager", "hubungkan ke pemilik toko", "mau ngobrol sama orang"} {
		if !IsHumanEscalationRequest(s) {
			t.Errorf("IsHumanEscalationRequest(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"who is the owner of this shop?", "Name: Budi\nPhone: 081234567890", "is the tuna frozen?", "ongkir ke surabaya berapa?"} {
		if IsHumanEscalationRequest(s) {
			t.Errorf("IsHumanEscalationRequest(%q) = true, want false", s)
		}
	}
}
```
- [ ] **Step 2: Run it.** Expected: FAIL on the first group.
- [ ] **Step 3: Implement.**
  - Extend `humanSupportTargetRe` to:
    ```
    (?i)\b(manusia|operator|agent|admin|administrator|owner|pemilik|manager|manajer|staff|staf|person|human|orang|penjual|seller)\b|customer service|\bcs\b|real person
    ```
  - Extend `humanHandoffVerbRe` with `contact|talk|speak|call|reach|complain`.
  - Keep `humanEscalationExcludedContext` unchanged.
  - Then check the negative group still passes. "who is the owner" has no request verb. If it still matches, require the verb to appear before the target.
- [ ] **Step 4: Run the whole package.** Expected: PASS. Apply the Task 1 Step 6 rule.
- [ ] **Step 5: Commit** `fix(buyerflow): kenali permintaan bicara dengan admin/owner/orang`.

---

### Task 4: Greeting period from words anywhere in a short greeting (O4, S9)

**Files:**
- Modify: `internal/buyerflow/greeting.go` (`DetectGreetingPeriodFromText` :54)
- Test: `internal/buyerflow/greeting_period_tokens_test.go` (new)

- [ ] **Step 1: Failing test**
```go
package buyerflow

import "testing"

func TestGreetingPeriodWordsAnywhere(t *testing.T) {
	cases := map[string]GreetingPeriod{"Hello morning": GreetMorning, "Hello evening": GreetEvening, "hi good afternoon": GreetAfternoon, "halo kak selamat malam": GreetNight}
	for msg, want := range cases {
		got, ok := DetectGreetingPeriodFromText(msg)
		if !ok || got != want {
			t.Errorf("DetectGreetingPeriodFromText(%q) = (%q,%v), want %q", msg, got, ok, want)
		}
	}
	if _, ok := DetectGreetingPeriodFromText("Hello"); ok {
		t.Error("bare hello has no period")
	}
}
```
- [ ] **Step 2: Run it.** Expected: FAIL for "Hello morning" and "Hello evening".
- [ ] **Step 3: Implement.** After the existing checks in `DetectGreetingPeriodFromText`, add a fallback for short texts (≤6 tokens): scan tokens for `morning|pagi` → `GreetMorning`, `afternoon|siang` → `GreetAfternoon`, `evening|sore` → `GreetEvening`, `night|malam` → `GreetNight`, and return the first one found.
- [ ] **Step 4: Run the whole package.** Expected: PASS.
- [ ] **Step 5: Commit** `fix(buyerflow): salam waktu ikut kata waktu dari pembeli`.

---

## C-order half

### Task 5: Questions mid-checkout are never stored as the recipient name (B4, S24, S26, S8)

**Files:**
- Modify: `internal/buyerflow/order_catalog.go`:
  - `parseRecipientLine` :488, at the assignment `name = line` near :527;
  - `mergeShippingText` :624, Street fallback near :711.
- Modify: `internal/buyerflow/order_parse.go`:
  - `orderAddrHintRe` :612;
  - `ShouldBreakOrderFlow` :615, specifically the name early-return near :702 and the address-hint check near :696.
- Modify: `internal/buyerflow/order_inline_multi.go` (`isProductQuestionNotCartAdd` :401)
- Test: `internal/buyerflow/checkout_question_gate_test.go` (new)

**Interfaces:**
- Produces: `func looksLikeBuyerQuestion(text string) bool` and `func plausibleNameLine(line string) bool` (unexported, in `order_catalog.go`).

- [ ] **Step 1: Failing test**
```go
package buyerflow

import "testing"

func TestRecipientNameRejectsQuestions(t *testing.T) {
	for _, s := range []string{
		"delivered today or tomorrow woi",
		"Is it going to be delivered today or tomorrow?",
		"is it frozen?",
		"Where do you ship from?",
		"how can i pay",
		"Heloo \nI want to ask about your tuna steak product",
		"kapan dikirim kak?",
	} {
		if name, _ := parseRecipientLine(s); name != "" {
			t.Errorf("parseRecipientLine(%q) name = %q, want empty", s, name)
		}
	}
	if name, phone := parseRecipientLine("Name: Anastasia\nPhone: 081234567890"); name != "Anastasia" || phone == "" {
		t.Fatalf("labelled data must still parse, got (%q,%q)", name, phone)
	}
	if name, _ := parseRecipientLine("Budi Santoso 081234567890"); name != "Budi Santoso" {
		t.Fatalf("plain name + phone must still parse, got %q", name)
	}
}

func TestQuestionsBreakCheckoutSteps(t *testing.T) {
	for _, step := range []string{"ask_recipient", "ask_address_full"} {
		for _, s := range []string{"delivered today or tomorrow woi", "is it frozen?", "what time do you start shipping"} {
			if !ShouldBreakOrderFlow(s, step, omahSteakCatalog()) {
				t.Errorf("ShouldBreakOrderFlow(%q, %s) = false, want true", s, step)
			}
		}
	}
	if ShouldBreakOrderFlow("Jl. Melati No 12, Malang, Jawa Timur 65145", "ask_address_full", omahSteakCatalog()) {
		t.Error("a real address must stay in the checkout step")
	}
}

func TestAddressHintNeedsWordBoundaries(t *testing.T) {
	for _, s := range []string{"what time do you start shipping", "is it for a party?", "smart choice"} {
		if orderAddrHintRe.MatchString(s) {
			t.Errorf("orderAddrHintRe matched %q", s)
		}
	}
	for _, s := range []string{"Jl. Melati 12", "RT 03 RW 05", "Kec. Sukolilo", "Kota Surabaya", "kode pos 60111"} {
		if !orderAddrHintRe.MatchString(s) {
			t.Errorf("orderAddrHintRe missed %q", s)
		}
	}
}

func TestProductQuestionIsNotCartAdd(t *testing.T) {
	for _, s := range []string{"how much is the tuna?", "is the tuna frozen?", "how much weight is the tuna?", "what is the price of the beef 500 gram?", "tunanya ready kak?"} {
		if !isProductQuestionNotCartAdd(s) {
			t.Errorf("isProductQuestionNotCartAdd(%q) = false, want true", s)
		}
	}
	if isProductQuestionNotCartAdd("tambah 1 tuna") {
		t.Error("explicit add is not a question")
	}
}
```
- [ ] **Step 2: Run it.** Expected: FAIL.
- [ ] **Step 3: Implement**
  - In `order_catalog.go`:
```go
var buyerQuestionStarts = map[string]bool{
	"what": true, "when": true, "where": true, "why": true, "how": true, "is": true, "are": true,
	"can": true, "could": true, "do": true, "does": true, "will": true, "would": true, "which": true,
	"apa": true, "apakah": true, "kapan": true, "dimana": true, "kenapa": true, "mengapa": true,
	"gimana": true, "bagaimana": true, "berapa": true, "bisakah": true, "bolehkah": true,
}

var nonNameWords = map[string]bool{
	"today": true, "tomorrow": true, "delivered": true, "deliver": true, "delivery": true,
	"ship": true, "shipping": true, "order": true, "cancel": true, "price": true, "frozen": true,
	"fresh": true, "ask": true, "want": true, "product": true, "kirim": true, "dikirim": true,
	"besok": true, "hari": true, "pesanan": true, "batal": true, "harga": true, "ongkir": true,
	"tanya": true, "mau": true, "woi": true, "pay": true, "bayar": true, "transfer": true,
}

// looksLikeBuyerQuestion — '?' anywhere, a question word first, or "X or Y" day choice.
func looksLikeBuyerQuestion(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if strings.Contains(t, "?") {
		return true
	}
	f := strings.Fields(t)
	if len(f) > 0 && buyerQuestionStarts[f[0]] {
		return true
	}
	return strings.Contains(t, "today or tomorrow") || strings.Contains(t, "hari ini atau besok")
}

// plausibleNameLine — 1–4 words, letters and . ' - only, no common non-name word.
func plausibleNameLine(line string) bool {
	f := strings.Fields(strings.TrimSpace(line))
	if len(f) == 0 || len(f) > 4 {
		return false
	}
	for _, w := range f {
		lw := strings.ToLower(strings.Trim(w, ".,'-"))
		if nonNameWords[lw] {
			return false
		}
		for _, r := range w {
			if !(unicode.IsLetter(r) || r == '.' || r == '\'' || r == '-') {
				return false
			}
		}
	}
	return true
}
```
    In `parseRecipientLine`, before scanning lines, return `("", phone-only result)` when `looksLikeBuyerQuestion(userText)` and the text has no `Name:`/`Nama:` label. Then replace the unlabelled `name = line` assignment with `if plausibleNameLine(line) { name = line }`. Keep the labelled-name branch unchanged. Add the `unicode` import if needed.
  - In `order_parse.go`:
    - Change `orderAddrHintRe` to `(?i)(\bjalan\b|\bjl\b\.?|\brt\b|\brw\b|\bkel\.|\bkec\.|\bkota\b|\bkab\.|\bkode pos\b|\btaman\b|setiabudi)`.
    - In `ShouldBreakOrderFlow`, at the recipient/address steps, add `if looksLikeBuyerQuestion(userText) && !HasCheckoutDataInMessage(userText) { return true }` before the name-based early `return false`.
  - In `order_inline_multi.go` `isProductQuestionNotCartAdd`:
    - Also return true when the text contains `?` or starts with a question word (reuse `looksLikeBuyerQuestion`);
    - and when it contains one of: how much, berapa, harga, price, frozen, fresh, beku, segar, weight, berat, ready, stock, stok, available, "is it", "is the".
    - In every case, only if it contains no explicit add/order verb (tambah, add, order, pesan, beli, mau, want) followed by a qty.
- [ ] **Step 4: Run the whole package.** Expected: PASS. Apply the Task 1 Step 6 rule.
- [ ] **Step 5: Commit** `fix(buyerflow): pertanyaan saat checkout tidak dijadikan nama penerima`.

---

### Task 6: Whole-order cancel phrases and English fillers (B6, S29)

**Files:**
- Modify: `internal/buyerflow/order_draft_mutate.go` (`isWholeOrderCancelPhrase` :22, `skuCancelFiller` :45)
- Modify (only if needed): `internal/buyerflow/order_intent.go` (`IsDraftOrderCancelRequest` :155)
- Test: `internal/buyerflow/cancel_phrases_test.go` (new)

- [ ] **Step 1: Failing test**
```go
package buyerflow

import "testing"

func TestWholeOrderCancelPhrases(t *testing.T) {
	for _, s := range []string{"please cancel my previous order", "cancel my previous order", "please cancel", "cancel the order", "can i cancel my order b", "batalkan pesanan sebelumnya", "batalin pesanan", "never mind"} {
		if !IsOrderCancelRequest(s) {
			t.Errorf("IsOrderCancelRequest(%q) = false, want true", s)
		}
		if IsCartLineCorrectionIntent(s) {
			t.Errorf("IsCartLineCorrectionIntent(%q) = true, want false (whole-order cancel)", s)
		}
	}
	for _, s := range []string{"cancel the tuna", "batalkan abon sapi", "remove the salmon"} {
		if !IsCartLineCorrectionIntent(s) {
			t.Errorf("IsCartLineCorrectionIntent(%q) = false, want true", s)
		}
	}
}
```
- [ ] **Step 2: Run it.** Expected: FAIL. For example, "please cancel my previous order" is a line correction today.
- [ ] **Step 3: Implement**
  - Add to `isWholeOrderCancelPhrase`'s list: "cancel my previous order", "cancel the previous order", "cancel previous order", "cancel my last order", "cancel the order", "cancel my orders", "please cancel", "batalkan pesanan sebelumnya", "batalin pesanan", "batal pesanan", "batalkan semua pesanan".
  - Add English fillers to `skuCancelFiller`: please, pls, my, the, previous, last, order, orders, it, this, that, all, everything, whole, entire, again, now, just, kindly, can, you, i, want, to, would, like, b.
  - If `IsOrderCancelRequest("please cancel")` is still false, add "please cancel" and "cancel please" to the phrase list that `IsDraftOrderCancelRequest` checks.
  - "remove the salmon" must be a line correction. If it is not, add `remove|delete|hapus` + SKU remainder handling next to the existing cancel-word logic in `lineCancelWithSKURemainder`, using the same remainder rule.
- [ ] **Step 4: Run the whole package.** Expected: PASS. Apply the Task 1 Step 6 rule.
- [ ] **Step 5: Commit** `fix(buyerflow): frasa batal seluruh pesanan bahasa Inggris dan Indonesia`.

---

### Task 7: Multi-item lines: units, trailing questions, ambiguous variants (S18, S23, S27)

**Files:**
- Modify: `internal/buyerflow/text_util.go` (`splitGluedMeasureToken` :24, `tokenize` :32)
- Modify: `internal/buyerflow/order_inline_multi.go` (`splitEnglishAndSegments` early `return nil` at :89; `parseAppendSegments` ambiguous-segment skip)
- Test: `internal/buyerflow/multi_item_lines_test.go` (new)

- [ ] **Step 1: Failing test**
```go
package buyerflow

import (
	"strings"
	"testing"
)

func TestUnitVariantsMatchCatalog(t *testing.T) {
	cat := omahSteakCatalog()
	cases := map[string]string{"1 of Australian beef 500grams": "beef-500", "1 salmon steak 100gr": "salmon-100", "salmon 250 grams": "salmon-250"}
	for msg, want := range cases {
		if it := matchCatalogItem(msg, cat); it == nil || it.ID != want {
			t.Errorf("matchCatalogItem(%q) = %v, want %s", msg, it, want)
		}
	}
}

func TestOrderSegmentsSurviveTrailingQuestion(t *testing.T) {
	s := newOmahSteakSimulator()
	s.SavedBuyerLang = LangEN
	s.Turn("Okay, can i order 1 tuna steak and 1 australian beef 500gr please?  And can you do same day delivery?")
	if s.Order == nil {
		t.Fatal("no order started")
	}
	ids := map[string]bool{}
	for _, l := range s.Order.Items {
		ids[l.CatalogItemID] = true
	}
	if s.Order.CatalogItemID != "" {
		ids[s.Order.CatalogItemID] = true
	}
	if !ids["tuna-100"] || !ids["beef-500"] {
		t.Fatalf("both lines must be kept, got %+v", s.Order)
	}
}

func TestAmbiguousAppendIsAskedNotDropped(t *testing.T) {
	s := newOmahSteakSimulator()
	s.SavedBuyerLang = LangEN
	s.Turn("Okay, can i order 1 tuna steak please?")
	out := s.Turn("i want too add Salmon steak 100 gr, and australian beef too")
	if !strings.Contains(out.Reply, "T-Bone") || !strings.Contains(out.Reply, "500 Gram") {
		t.Fatalf("ambiguous 'australian beef' must be asked with its two variants, reply=%q", out.Reply)
	}
}
```
- [ ] **Step 2: Run it.** Expected: FAIL.
- [ ] **Step 3: Implement**
  - `tokenize` / `splitGluedMeasureToken`: normalise unit tokens after splitting with `gr|grs|grm|grams|gramm → gram`, `kgs → kg`, `pcs → pcs`. Apply the same mapping to standalone tokens, so "500 grams" also becomes `500 gram`.
  - `splitEnglishAndSegments` (:89):
    - Do not abort the whole split when a later segment is a question (`?` or `questionSegmentStartRe`).
    - Drop that question segment and keep the earlier order segments.
    - Return `nil` only when no order segment remains.
  - `parseAppendSegments`: when a segment is skipped because `lexicalBrandAmbiguous` is true, collect it. If any segment was collected, append a variant question to the reply. Reuse `formatBrandVariantListBody(formal, brandToken, items, 5, userText)`; it is language-aware via `PickBuyerLanguageReply`. Do not silently drop it.
- [ ] **Step 4: Run the whole package.** Expected: PASS. Apply the Task 1 Step 6 rule.
- [ ] **Step 5: Commit** `fix(buyerflow): baris pesanan ganda tidak hilang (satuan, pertanyaan, varian ambigu)`.

---

### Task 8: Upsell at most one product, once per order (O1, S33)

**Files:**
- Modify: `internal/buyerflow/types.go` (`OrderState` :83, add the field)
- Modify: `internal/buyerflow/sales_format.go` (`formatUpsellBlock` :482)
- Modify: `internal/buyerflow/order_fsm.go` (`AdvanceOrderFlow` :53)
- Test: `internal/buyerflow/upsell_once_test.go` (new)

**Interfaces:**
- Produces: `OrderState.UpsellShown bool \`json:"upsellShown,omitempty"\``. `formatUpsellBlock` returns `""` when `st.UpsellShown` is set, and otherwise returns at most 1 suggestion. `AdvanceOrderFlow` sets `UpsellShown` on the returned state when its reply contains the upsell header.

- [ ] **Step 1: Failing test**
```go
package buyerflow

import (
	"strings"
	"testing"
)

func upsellCount(reply string) int {
	return strings.Count(reply, "You might also like") + strings.Count(reply, "Rekomendasi tambahan")
}

func TestUpsellOncePerOrderAndSingleItem(t *testing.T) {
	s := newOmahSteakSimulator()
	s.SavedBuyerLang = LangEN
	first := s.Turn("Okay, can i order 1 tuna steak please")
	second := s.Turn("i want to add 1 salmon steak 100 gram")
	total := upsellCount(first.Reply) + upsellCount(second.Reply)
	if total > 1 {
		t.Fatalf("upsell shown %d times, want at most once:\n1) %s\n2) %s", total, first.Reply, second.Reply)
	}
	if strings.Count(first.Reply+second.Reply, "\n1. ") > 0 && strings.Contains(first.Reply+second.Reply, "\n2. ") && upsellCount(first.Reply+second.Reply) == 1 {
		blockStart := strings.Index(first.Reply+second.Reply, "You might also like")
		if blockStart >= 0 && strings.Contains((first.Reply+second.Reply)[blockStart:], "\n2. ") {
			t.Fatal("upsell block must contain one suggestion")
		}
	}
}
```
- [ ] **Step 2: Run it.** Expected: FAIL, because the upsell appears twice.
- [ ] **Step 3: Implement**
  - Add the `UpsellShown` field.
  - In `formatUpsellBlock`, return `""` if `st.UpsellShown`, and call `suggestRelatedProducts(st, catalog, 1)`.
  - Rename the current `AdvanceOrderFlow` body to `advanceOrderFlow` and add:
```go
func AdvanceOrderFlow(in OrderFlowInput, persist persistOrderFunc) OrderFlowResult {
	res := advanceOrderFlow(in, persist)
	if res.State != nil && (strings.Contains(res.Reply, "You might also like") || strings.Contains(res.Reply, "Rekomendasi tambahan")) {
		res.State.UpsellShown = true
	}
	return res
}
```
    Check the exact header strings in `formatUpsellBlock` and use the same literals, for example via shared consts.
- [ ] **Step 4: Run the whole package.** Expected: PASS. Tests that count upsell blocks (`steak_order_script_test.go:106`, `omah_cadbury_thread_test.go:270`) may need updated expectations. Apply the Task 1 Step 6 rule.
- [ ] **Step 5: Commit** `fix(buyerflow): upsell maksimal satu produk, sekali per pesanan`.

---

### Task 9: Digits in recipient/address steps are not a quantity revision (O2, S25)

**Files:**
- Modify: `internal/buyerflow/order_qty.go` (`tryApplyQtyRevision` :41)
- Test: `internal/buyerflow/qty_vs_phone_test.go` (new)

- [ ] **Step 1: Failing test**
```go
package buyerflow

import "testing"

func TestPhoneAndHouseNumbersAreNotQty(t *testing.T) {
	for _, step := range []string{"ask_recipient", "ask_address_full"} {
		for _, msg := range []string{"Name: Budi\nPhone: 0812 3456 7890", "+62 812 3456 7890", "Jl. Melati 12", "Jl. Melati No 12"} {
			st := &OrderState{Step: step, Qty: 1, ProductName: "Yellow Tail Tuna Portion 100 Gram", CatalogItemID: "tuna-100", UnitPrice: 350000}
			if tryApplyQtyRevision(st, msg) || st.Qty != 1 {
				t.Errorf("step %s msg %q changed qty to %d", step, msg, st.Qty)
			}
		}
	}
	st := &OrderState{Step: "ask_recipient", Qty: 1, ProductName: "Yellow Tail Tuna Portion 100 Gram", CatalogItemID: "tuna-100"}
	if !tryApplyQtyRevision(st, "jadi 2 pcs aja") || st.Qty != 2 {
		t.Fatalf("explicit qty change must still work, qty=%d", st.Qty)
	}
}
```
- [ ] **Step 2: Run it.** Expected: FAIL.
- [ ] **Step 3: Implement.** At the top of `tryApplyQtyRevision`, when `st.Step` is `ask_recipient`, `ask_address` or `ask_address_full`, require an explicit qty cue. If the lowercased text contains none of `pcs`, `pc`, `buah`, `biji`, `porsi`, `pack`, `paket`, `qty`, `jumlah`, `jadi `, `ubah`, `revisi`, `ganti`, `make it`, `change to`, or a `\b\d+\s*x\b` pattern, return false.
- [ ] **Step 4: Run the whole package.** Expected: PASS. Apply the Task 1 Step 6 rule.
- [ ] **Step 5: Commit** `fix(buyerflow): angka HP/alamat tidak dibaca sebagai revisi jumlah`.
