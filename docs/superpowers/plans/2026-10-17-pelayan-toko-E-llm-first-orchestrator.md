# Pelayan Toko — Part E: LLM-first orchestrator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the per-tenant LLM-first reply pipeline (spec §3.1). It makes one JSON LLM call per buyer turn, grounded in the full catalog and KB. Code enforces the language lock, open-order reminders, cart and field operations, and an honest handoff. The pipeline fails open to the legacy flow and never pauses on errors.

**Architecture:**
- **Pure decision core** — `ai/llm_first_plan.go`, `planTurn`.
  - It receives already-loaded inputs and an injected chat function, and returns a `turnPlan`. Nothing is written and nothing is sent.
  - It is unit-tested with a fake LLM against the incident transcripts.
- **Thin I/O shell** — `ai/llm_first.go`. It loads data, calls `planTurn`, applies side effects and sends.
- **Shadow mode** — runs `planTurn` only and logs the result.
- **Flags** — `ai_llm_first`, `ai_llm_first_shadow` and `ai_llm_first_kill`, with negative caching added to the `flag` package.

**Tech Stack:** Go 1.26, Encore.
- `internal/salesbrain` (pure, `go test`).
- Package `ai` and package `flag` tests run with `encore test ./ai/ -run …` and `encore test ./flag/ -run …` (Docker is up).
- `llm.RunChat` and `ChatRequest.JSON`/`Temperature` come from Part B.
- Part D's helpers are used: `notifyOwner`, `HandoffReasonHumanRequest`, `TplCheckWithTeam`, `kbForLLMPrompt`, `lastOutboundWasLanguageAsk`, `lastAIOutboundBody`.
- Part C's buyerflow changes are used: `OrderState.UpsellShown`, `plausibleNameLine`.

Spec: `docs/superpowers/specs/2026-10-17-pelayan-toko-llm-first-design.md` (all sections).

## Global Constraints

- **Worktree:** `/Users/viko/Documents/WABantu/.worktrees/api-go-pelayan-toko`, branch `feat/pelayan-toko-llm-first`. Parts B, C and D are merged into it before this plan starts. Check with `git log --oneline | head -30`; it must show their commits. If they are missing, stop.
- **No schema migrations, no new secrets or env vars, no new `//encore:api` endpoints.**
- **Defaults:**
  - Every flag defaults to OFF. A tenant without the flag must behave exactly as before.
  - The kill flag `ai_llm_first_kill` (`enabled_globally=true`) disables the pipeline for everyone.
- **Fail-open:** any error before the first side effect returns `handled=false, err=nil`, so the legacy flow handles the turn. Never pause the AI because of a pipeline error.
- **Language:** `salesbrain.DecideLanguage` is the only language authority. The model's `language_switch` is logged and never applied by itself.
- **Money:** amounts are computed by code. The model never writes the summary or the total; code appends `salesbrain.RenderCartSummary`.
- **Privacy:**
  - The prompt's STATE block never contains the stored phone or address.
  - `rawOutput` in message metadata is redacted with `salesbrain.RedactForLog` and capped at 1000 runes.
- **Cost:**
  - Use the `chat` chain via `llm.RunChat` with `MaxTokens 1024`, `JSON true`, `Temperature 0.3`, a 15s timeout per call, and at most 2 calls per turn (1 retry).
  - Record usage: `ai_token` = input + output + thoughts.
- **Commits:** messages in Indonesian with the trailer `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Use `set -o pipefail` when piping. Never print secrets or touch `.env*` files. Do not push.

---

### Task 1: salesbrain helpers for cart, summary, resume lines, redaction and address fields

**Files:**
- Modify: `internal/salesbrain/openorder.go` (add `ID` to `OrderLine`), `internal/salesbrain/validate.go` (add `ID` to `CatalogFact`), `internal/salesbrain/types.go` (add `Fields` to `Decision`), `internal/salesbrain/prompt.go` (contract text for `fields`).
- Create: `internal/salesbrain/cart.go`, `internal/salesbrain/redact.go`
- Test: `internal/salesbrain/cart_test.go`, `internal/salesbrain/redact_test.go`, and an addition to `output_test.go`

**Interfaces (produce exactly):**
```go
// openorder.go
type OrderLine struct {
	ID        string // catalog item id ("" for legacy rows without id)
	Name      string
	Qty       int
	UnitPrice int64
}
// validate.go
type CatalogFact struct { No int; ID string; Name string; Price int64; Unit, Stock, Description, Code string }
// types.go
type FieldData struct {
	Name       string `json:"name"`
	Phone      string `json:"phone"`
	Street     string `json:"street"`
	District   string `json:"district"`
	City       string `json:"city"`
	Province   string `json:"province"`
	PostalCode string `json:"postal_code"`
}
// Decision gains: Fields *FieldData `json:"fields"`
// cart.go
func ApplyItemOps(cart []OrderLine, ops []ItemOp, catalog []CatalogFact) (out []OrderLine, changed bool, unknown []int)
func CartSubtotal(cart []OrderLine) int64
func RenderCartSummary(cart []OrderLine, lang Lang) string
func ResumeLineFor(field string, lang Lang) string // "recipient"|"address"|"confirm" → template text, else ""
// redact.go
func RedactForLog(s string, maxRunes int) string
```

- [ ] **Step 1: Failing tests** in `internal/salesbrain/cart_test.go`:
```go
package salesbrain

import (
	"strings"
	"testing"
)

var steakFacts = []CatalogFact{
	{No: 1, ID: "beef-500", Name: "Australian Beef 500 Gram", Price: 450000},
	{No: 2, ID: "tbone-1500", Name: "Australian Beef T-Bone 1.5 Kg", Price: 1750000},
	{No: 3, ID: "salmon-100", Name: "Salmon Steak 100 Gram", Price: 75000},
	{No: 4, ID: "salmon-250", Name: "Salmon Steak 250 Gram", Price: 105000},
	{No: 5, ID: "tuna-100", Name: "Yellow Tail Tuna Portion 100 Gram", Price: 350000},
}

func TestApplyItemOps(t *testing.T) {
	cart, changed, unknown := ApplyItemOps(nil, []ItemOp{{No: 5, Qty: 1}, {No: 3, Qty: 2}, {No: 9, Qty: 1}}, steakFacts)
	if !changed || len(cart) != 2 || cart[0].ID != "tuna-100" || cart[1].Qty != 2 || len(unknown) != 1 || unknown[0] != 9 {
		t.Fatalf("got cart=%+v changed=%v unknown=%v", cart, changed, unknown)
	}
	if CartSubtotal(cart) != 350000+150000 {
		t.Fatalf("subtotal %d", CartSubtotal(cart))
	}
	cart, changed, _ = ApplyItemOps(cart, []ItemOp{{No: 3, Qty: 0}}, steakFacts)
	if !changed || len(cart) != 1 || cart[0].ID != "tuna-100" {
		t.Fatalf("qty 0 must remove the line, got %+v", cart)
	}
	_, changed, _ = ApplyItemOps(cart, []ItemOp{{No: 5, Qty: 1}}, steakFacts)
	if changed {
		t.Fatal("same qty must not count as a change")
	}
}

func TestRenderCartSummary(t *testing.T) {
	cart := []OrderLine{{ID: "salmon-100", Name: "Salmon Steak 100 Gram", Qty: 1, UnitPrice: 75000}, {ID: "tuna-100", Name: "Yellow Tail Tuna Portion 100 Gram", Qty: 1, UnitPrice: 350000}}
	en := RenderCartSummary(cart, LangEN)
	for _, want := range []string{"Order summary", "1x Salmon Steak 100 Gram", "Rp75.000", "Subtotal: Rp425.000", "shipping not included"} {
		if !strings.Contains(en, want) {
			t.Errorf("EN summary missing %q:\n%s", want, en)
		}
	}
	id := RenderCartSummary(cart, LangID)
	for _, want := range []string{"Ringkasan pesanan", "Subtotal: Rp425.000", "belum termasuk ongkir"} {
		if !strings.Contains(id, want) {
			t.Errorf("ID summary missing %q:\n%s", want, id)
		}
	}
	if RenderCartSummary(nil, LangEN) != "" {
		t.Error("empty cart renders nothing")
	}
}

func TestResumeLineFor(t *testing.T) {
	if ResumeLineFor("address", LangEN) != T(TplResumeAddress, LangEN) || ResumeLineFor("recipient", LangID) != T(TplResumeRecipient, LangID) || ResumeLineFor("", LangEN) != "" {
		t.Fatal("resume line mapping wrong")
	}
}
```
In `internal/salesbrain/redact_test.go`:
```go
package salesbrain

import (
	"strings"
	"testing"
)

func TestRedactForLog(t *testing.T) {
	in := `{"reply":"Thanks Ana, phone 0812 3456 7890 and +62 812-3456-7890 noted; email ana@example.com; total Rp425.000"}`
	got := RedactForLog(in, 1000)
	for _, leak := range []string{"0812 3456 7890", "812-3456-7890", "ana@example.com"} {
		if strings.Contains(got, leak) {
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
	if !strings.Contains(got, "Rp425.000") {
		t.Errorf("amounts must survive: %q", got)
	}
	if len([]rune(RedactForLog(strings.Repeat("a", 2000), 1000))) > 1001 {
		t.Error("must truncate")
	}
}
```
Add this to `output_test.go`:
```go
func TestParseDecisionFields(t *testing.T) {
	d, err := ParseDecision(`{"intent":"field_answer","fields":{"name":"Ana","phone":"0872839472828","street":"Villa Kalijudan Indah A9","city":"Surabaya","province":"Jawa Timur","postal_code":"60177"},"reply":"Thanks Ana."}`)
	if err != nil || d.Fields == nil || d.Fields.City != "Surabaya" || d.Fields.PostalCode != "60177" {
		t.Fatalf("got %+v err=%v", d, err)
	}
	d, _ = ParseDecision(`{"intent":"question","fields":{"name":"X"},"reply":"ok"}`)
	if d.Fields != nil {
		t.Fatal("fields only kept for field_answer")
	}
}
```
- [ ] **Step 2: Run** `go test ./internal/salesbrain/ -count=1`. Expected: FAIL to compile.
- [ ] **Step 3: Implement**
  - Add the fields.
  - In `ParseDecision`, set `d.Fields = nil` unless `d.Intent == IntentFieldAnswer`, and trim every field.
  - Extend the prompt's `KELUARAN` contract with `"fields": {"name","phone","street","district","city","province","postal_code"}`. Add the rule: "fields hanya untuk intent field_answer; isi persis dari pesan pembeli, kosongkan yang tidak disebut, jangan mengarang".
  - `cart.go`:
```go
package salesbrain

import (
	"fmt"
	"strings"
)

// ApplyItemOps sets the desired quantity per catalog row (qty 0 removes the line). Unknown row
// numbers are reported, not applied. changed is false when nothing effectively changed.
func ApplyItemOps(cart []OrderLine, ops []ItemOp, catalog []CatalogFact) ([]OrderLine, bool, []int) {
	byNo := make(map[int]CatalogFact, len(catalog))
	for _, c := range catalog {
		byNo[c.No] = c
	}
	out := append([]OrderLine(nil), cart...)
	changed := false
	var unknown []int
	for _, op := range ops {
		c, ok := byNo[op.No]
		if !ok {
			unknown = append(unknown, op.No)
			continue
		}
		idx := -1
		for i, l := range out {
			if (l.ID != "" && l.ID == c.ID) || (l.ID == "" && strings.EqualFold(l.Name, c.Name)) {
				idx = i
				break
			}
		}
		switch {
		case op.Qty == 0 && idx >= 0:
			out = append(out[:idx], out[idx+1:]...)
			changed = true
		case op.Qty > 0 && idx >= 0:
			if out[idx].Qty != op.Qty || out[idx].UnitPrice != c.Price {
				out[idx].Qty, out[idx].UnitPrice = op.Qty, c.Price
				changed = true
			}
		case op.Qty > 0:
			out = append(out, OrderLine{ID: c.ID, Name: c.Name, Qty: op.Qty, UnitPrice: c.Price})
			changed = true
		}
	}
	return out, changed, unknown
}

// CartSubtotal sums qty × unit price.
func CartSubtotal(cart []OrderLine) int64 {
	var s int64
	for _, l := range cart {
		s += int64(l.Qty) * l.UnitPrice
	}
	return s
}

// RenderCartSummary is the code-rendered order summary appended after the model's reply.
func RenderCartSummary(cart []OrderLine, lang Lang) string {
	if len(cart) == 0 {
		return ""
	}
	var b strings.Builder
	if lang == LangEN {
		b.WriteString("Order summary:\n")
	} else {
		b.WriteString("Ringkasan pesanan:\n")
	}
	for i, l := range cart {
		fmt.Fprintf(&b, "%d. %dx %s — %s\n", i+1, l.Qty, l.Name, FormatRupiah(int64(l.Qty)*l.UnitPrice))
	}
	if lang == LangEN {
		fmt.Fprintf(&b, "Subtotal: %s (shipping not included)", FormatRupiah(CartSubtotal(cart)))
	} else {
		fmt.Fprintf(&b, "Subtotal: %s (belum termasuk ongkir)", FormatRupiah(CartSubtotal(cart)))
	}
	return b.String()
}

// ResumeLineFor re-asks a pending checkout field in the buyer language.
func ResumeLineFor(field string, lang Lang) string {
	switch field {
	case "recipient":
		return T(TplResumeRecipient, lang)
	case "address":
		return T(TplResumeAddress, lang)
	case "confirm":
		return T(TplResumeConfirm, lang)
	}
	return ""
}
```
  - `redact.go`: replace emails (`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`) with `[EMAIL]`. Replace digit runs of at least 8 digits, allowing spaces, dashes, dots and parentheses between digits, with `[NUM]`. Do not touch `Rp` amounts: skip any match that `rpAmountRe` from validate.go would capture, or apply the number rule only to matches not preceded by `Rp`. Then truncate to `maxRunes` with `…`.
- [ ] **Step 4: Run** `go test ./internal/salesbrain/ -count=1 -race` and `go vet ./internal/salesbrain/`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(salesbrain): operasi keranjang, ringkasan, kalimat lanjut, dan redaksi log`.

---

### Task 2: Flag keys and negative caching

**Files:**
- Modify: `flag/flag.go` `IsEnabled` (≈:231-245): cache misses as a disabled flag (60s).
- Create: `flag/llm_first.go` with the constants.
- Test: `flag/llm_first_test.go` (`encore test`)

**Interfaces:** `const FlagLLMFirst = "ai_llm_first"; FlagLLMFirstShadow = "ai_llm_first_shadow"; FlagLLMFirstKill = "ai_llm_first_kill"`. `func LLMFirstMode(ctx context.Context, tenantID string) string` returns `"off"`, `"shadow"` or `"on"`. Order of checks:
1. kill enabled (global) → `"off"`;
2. `ai_llm_first` enabled for the tenant → `"on"`;
3. `ai_llm_first_shadow` enabled for the tenant → `"shadow"`;
4. otherwise `"off"`.

- [ ] **Step 1: Failing test.** Follow the existing flag test style (read `flag/*_test.go` first).
  - Unknown keys return false and a second call does not hit the DB. Assert this via a package-level query counter you add for tests, or by checking `getCached` after the first call.
  - `LLMFirstMode` returns `"off"` when no rows exist.
- [ ] **Step 2: Run** `encore test ./flag/ -run 'LLMFirst|NegativeCache' -count=1`. Expected: FAIL.
- [ ] **Step 3: Implement.** On `sql.ErrNoRows` in `IsEnabled`, `setCache(FeatureFlag{Key: key})` before returning false. Do not cache other DB errors. Add the constants and `LLMFirstMode`.
- [ ] **Step 4: Run** the tests. Expected: PASS. Run `go build ./...`.
- [ ] **Step 5: Commit** `feat(flag): flag ai_llm_first beserta cache untuk flag yang tidak ada`.

---

### Task 3: `planTurn` — the pure LLM-first decision core

**Files:**
- Create: `ai/llm_first_plan.go`
- Test: `ai/llm_first_plan_test.go` (`encore test`; the function itself needs no DB)

**Interfaces (produce exactly):**
```go
type chatFunc func(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error)

type planInput struct {
	Lang          salesbrain.LanguageDecision
	LangLocked    bool
	Profile       salesbrain.ProfileFact
	Catalog       []salesbrain.CatalogFact
	KB            []salesbrain.KBFact
	State         salesbrain.TurnState // Cart/PendingField/OpenOrders/RemindAllowed already filled
	History       []salesbrain.HistoryTurn
	Message       string
	BuyerTexts    []string // current + up to 3 previous buyer messages
}

type turnPlan struct {
	Decision     salesbrain.Decision
	Lang         salesbrain.Lang
	Cart         []salesbrain.OrderLine // cart after item ops (== State.Cart when unchanged)
	CartChanged  bool
	UnknownItems []int
	Reply        string // final text to send (model reply + code additions)
	Fallback     bool   // true → do not send; legacy flow handles the turn
	FallbackWhy  string
	Attempts     int
	Usage        llm.Usage // summed across attempts (Input/Output/Thoughts)
	RawOutput    string    // last raw model output, redacted
	Violations   []salesbrain.Violation // of the last attempt
}

func planTurn(ctx context.Context, in planInput, chat chatFunc) turnPlan
```
`planTurn` algorithm. Implement it exactly:
1. `system, user := salesbrain.BuildPrompt(salesbrain.PromptInput{Profile: in.Profile, Catalog: in.Catalog, KB: in.KB, State: in.State, History: in.History, Message: in.Message})`. If `in.Lang.Switched`, prepend to `user` the line `CATATAN SISTEM: pembeli baru saja memilih bahasa ini; buka balasan dengan satu kalimat konfirmasi dalam bahasa ini.`
2. Make up to 2 attempts. Each attempt:
   - Call `chat(ctx, llm.ChatRequest{System: system, Messages: []llm.Message{{Role: "user", Text: user + note}}, MaxTokens: 1024, JSON: true, Temperature: &temp03})`. `note` is empty on attempt 1.
   - Add the call's usage to the running total.
   - If the error is non-nil, return `Fallback` with `why "llm_error"`.
   - `d, err := salesbrain.ParseDecision(resp.Text)`. If it fails, set `note = "\n\nCATATAN SISTEM: balasan sebelumnya bukan JSON valid. Kirim HANYA satu objek JSON sesuai format KELUARAN."` and continue.
   - When `d.Intent` is order or amend, run `cart, changed, unknown := salesbrain.ApplyItemOps(in.State.Cart, d.Items, in.Catalog)`.
   - `facts := salesbrain.BuildFacts(salesbrain.FactsInput{Catalog: in.Catalog, KB: in.KB, Cart: cart, CartSubtotal: salesbrain.CartSubtotal(cart), OpenOrders: in.State.OpenOrders, BuyerTexts: in.BuyerTexts})`.
   - `vs := salesbrain.Validate(d, facts, in.Lang.Lang)`.
   - If `len(vs) == 0`, accept and go to step 3. Otherwise set `note = "\n\n" + salesbrain.ViolationNote(vs)` and continue.
3. If no attempt was accepted, return `Fallback` with `why "invalid_output"`. Keep `Violations` and `RawOutput`.
4. Compose `Reply` from `d.Reply`:
   - If `CartChanged`, append `"\n\n" + salesbrain.RenderCartSummary(cart, lang)`.
   - If the next pending field is non-empty and `d.Reply` contains no `?`, append `"\n\n" + salesbrain.ResumeLineFor(nextField, lang)`. Compute the next field as follows:
     - After an item change with a non-empty cart whose `State.PendingField` is `""`, it becomes `"recipient"`.
     - Otherwise it stays `State.PendingField`.
     - An empty cart has no pending field.
   - Validate again only for `VTooLong`. If the composed reply is longer than 1400 runes, drop the summary rather than the answer.
5. `Lang` is always `in.Lang.Lang`. The model's `language_switch` is ignored.

- [ ] **Step 1: Failing tests** in `ai/llm_first_plan_test.go`.
  - Use a fake `chatFunc` that returns queued responses and records requests.
  - Build `planInput` from the omah steak catalog (5 items, with the descriptions from the incident: "Sold frozen and vacuum-sealed") and the 10 omah KB rows. The KB rows include `BCA - 000111222 - Omah Apparel` and `Mandiri - 333111222 - Omah Apparel`, the same-day 08:00–12:00 rule and the 3–4 day ETA.
  - Tests (all must exist):
    1. `TestPlanFrozenQuestionAnsweredFromDescription`. EN locked, message "Is it frozen or freshly cut?". The fake returns `{"intent":"question","reply":"It's sold frozen and vacuum-sealed, not freshly cut. Anything else you'd like to know?"}`. Assert:
       - `!Fallback`;
       - the reply contains "frozen";
       - the request's system prompt contains "Sold frozen and vacuum-sealed";
       - the request has `JSON == true` and `MaxTokens == 1024`.
    2. `TestPlanOrderAppliesItemsAndAppendsSummary`. EN, message "Okay, i would like to order 1 tuna steak please". The fake returns `{"intent":"order","items":[{"no":5,"qty":1}],"reply":"Sure, added 1x Yellow Tail Tuna Portion 100 Gram."}`. Assert:
       - `CartChanged`;
       - `Cart[0].ID == "tuna-100"`;
       - the reply contains "Order summary", "Rp350.000" and the EN recipient resume line.
    3. `TestPlanRetriesOnInventedPrice`. The first response quotes "Rp300.000" for tuna and the second is correct. Assert:
       - `Attempts == 2`;
       - the second request contains "Nominal Rp300.000";
       - `!Fallback`.
    4. `TestPlanFallsBackOnBrokenJSONTwice`. Both responses are `"not json"`. Assert `Fallback` and `FallbackWhy == "invalid_output"`.
    5. `TestPlanRejectsWrongLanguage`. EN locked. The first response is Indonesian (`"Siap kak, mau pesan yang mana dulu ya?"`) and the second is English. Assert `Attempts == 2` and `!Fallback`.
    6. `TestPlanMidCheckoutQuestionKeepsCartAndResumes`. `State.PendingField = "address"` with a cart of 1 tuna. Message "delivered today or tomorrow woi". The fake returns a question answer with no "?". Assert:
       - `!CartChanged`;
       - the reply ends with the EN address resume line.
    7. `TestPlanLLMErrorFallsBack`. The chat returns an error. Assert `Fallback` and `FallbackWhy == "llm_error"`.
- [ ] **Step 2: Run** `encore test ./ai/ -run TestPlan -count=1`. Expected: FAIL (undefined).
- [ ] **Step 3: Implement** `ai/llm_first_plan.go` per the algorithm. `temp03 := 0.3`. Sum usage with `u.InputTokens += r.Usage.InputTokens`, and the same for `OutputTokens` and `ThoughtsTokens`. Keep `Usage.Model` and `Usage.Provider` from the last attempt. `RawOutput = salesbrain.RedactForLog(resp.Text, 1000)`.
- [ ] **Step 4: Run** the tests. Expected: PASS.
- [ ] **Step 5: Commit** `feat(ai): inti keputusan LLM-first (planTurn) dengan retry dan validasi`.

---

### Task 4: Order adapter — apply a plan to OrderState, drafts and handoff

**Files:**
- Create: `ai/llm_first_orders.go`
- Modify: `internal/buyerflow/export.go`. Export wrappers only when no exported equivalent exists: `ParseRecipientLine`, `MergeShippingText`, `NormalizePhoneID`, `PlausibleNameLine`, `ShippingComplete` (method already exported).
- Test: `ai/llm_first_orders_test.go` (`encore test`; pure functions)

**Interfaces (produce exactly):**
```go
// cartFromState / stateWithCart convert between bf.OrderState and salesbrain lines.
func cartFromState(st *orderState) []salesbrain.OrderLine
func stateWithCart(st *orderState, cart []salesbrain.OrderLine, catalog []dbCatalogItem) *orderState // nil when cart empty
func pendingFieldFromState(st *orderState) string // "", "recipient", "address", "confirm"
// applyFieldAnswer validates model-extracted fields against the buyer text and writes the valid
// ones; returns the problems in the buyer language (e.g. "the phone number has 9 digits…").
func applyFieldAnswer(st *orderState, f *salesbrain.FieldData, buyerText string, lang salesbrain.Lang) (problems []string)
```
Rules:
- **`stateWithCart`**
  - Sets `st.Items` to one line per cart line. Lines are `OrderLineState{CatalogItemID, ExternalCode, ProductName, Qty, UnitPrice, SellUnit}`; take `ExternalCode` and `SellUnit` from the catalog row with the same id.
  - When there is exactly one line, it mirrors the single-item fields (`CatalogItemID`, `ProductName`, `Qty`, `UnitPrice`, `SellUnit`, `ExternalCode`), as legacy code expects.
  - Moves `Step` from `""`, `ask_product`, `ask_variant` or `ask_qty` to `"ask_recipient"`.
  - Keeps the recipient and address fields.
- **`applyFieldAnswer`**
  - A field is written only if its value also appears in `buyerText`, compared case-insensitively. For phone and postal code, compare digits only. This is the "no invented data" rule.
  - Phone: `NormalizePhoneID`, then 10–13 digits starting with `08` or `628`. Otherwise add the problem "nomor HP <x> belum valid (…digit)" or its EN equivalent.
  - Name: `PlausibleNameLine`.
  - Postal code: exactly 5 digits.
  - Street, city, province and district: non-empty.
  - Step transitions:
    - `ask_recipient` moves to `ask_address_full` once the name and phone are both set.
    - Any step moves to `"confirm"` when `st.ShippingComplete()`.
- **`pendingFieldFromState`** maps `ask_recipient` → recipient, `ask_address`/`ask_address_full` → address, and `confirm` → confirm.

- [ ] **Step 1: Failing tests:**
  - A round trip `cartFromState(stateWithCart(nil, cart, catalog))` returns the same cart.
  - A single line mirrors the single-item fields.
  - A field answer with name "Ana", phone "0872839472828", street "Villa Kalijudan Indah A9", city "Surabaya", province "Jawa Timur" and postal "60177", where all values appear in `buyerText`, ends in step `confirm`.
  - An invented city that is not in `buyerText` is not written.
  - Phone "98822123" produces one problem in EN that mentions the digits.
  - A question text never changes the name.
- [ ] **Step 2: Run** `encore test ./ai/ -run 'LLMFirstOrders|FieldAnswer|CartFromState' -count=1`. Expected: FAIL.
- [ ] **Step 3: Implement** using the bf wrappers and the existing alias `orderState = bf.OrderState`. Add the export wrappers to `internal/buyerflow/export.go` following its existing style, and run `go test ./internal/buyerflow/ -count=1`.
- [ ] **Step 4: Run.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(ai): adapter keranjang dan data penerima untuk LLM-first`.

---

### Task 5: The I/O shell — hook, loads, side effects, send, usage, shadow

**Files:**
- Create: `ai/llm_first.go`
- Modify: `ai/autoreply.go`. Add one hook right after the `IsThirdPartyBuyerLookup` gate (≈:268). Keep it at most about 10 lines:
```go
	if handled, err := s.tryLLMFirst(ctx, ts, payload, convo, channel, contact, profile, userText); handled || err != nil {
		return handled, err
	}
```
- Modify: `ai/reply_meta.go`. Add `Pipeline string \`json:"pipeline,omitempty"\``, `Intent string \`json:"intent,omitempty"\``, `Lang string \`json:"lang,omitempty"\``, `ReplacedReason string \`json:"replacedReason,omitempty"\``, `RawOutput string \`json:"rawOutput,omitempty"\``, `Attempts int \`json:"attempts,omitempty"\``.
- Modify: `ai/buyerflow_bridge.go`. Add `PathLLMFirst = "llm_first"` next to the other path aliases (define it in ai if buyerflow has no constant). Also add it to the LLM path lists in `ai/reply_meta.go` (`metaFromRoute`) and `ai/triage.go` (≈:94-98).
- Test: `ai/llm_first_test.go` (`encore test`). Unit-test the pure helpers below. End-to-end DB tests are optional: add them if an existing ai test harness with DB fixtures exists (search `ai/*_test.go` for `openTenantScope` usage).

**`tryLLMFirst` contract.** Return `(false, nil)` whenever the legacy flow should handle the turn.
1. `mode := appflag.LLMFirstMode(ctx, payload.TenantID)`.
   - If `mode == "shadow"`, start `go s.shadowLLMFirst(context.WithoutCancel(ctx), …)` with a 20s timeout and return `(false, nil)`.
   - If `mode != "on"`, return `(false, nil)`.
2. Cost gate. `usage.CheckAICostLimit`. When denied, send `salesbrain.T(TplQuotaExhausted, lang, shop)` with path `PathCostLimit` and return `true`.
3. Loads, all read-only:
   - history: `loadHistory(..., 12)`;
   - `lastAIOutboundBody`;
   - catalog: the same functions and sequence the legacy flow uses before its LLM call, i.e. `loadActiveCatalog`, tier prices and stock enrichment. Find them in `ProcessAutoReply` and reuse them unchanged;
   - KB: `loadKBEntries(ctx, ts, 50)`;
   - `s.getOrderState`;
   - open orders: `loadActiveOrdersForContact(ctx, ts, payload.TenantSchema, orderAccessScope{ConversationID: convo.ID, ContactID: convo.ContactID})`;
   - reminder state from Redis key `ai:openorder:remind:<tenantID>:<convoID>` (JSON `{lastAt, count}`).

   Any load error returns `(false, nil)` and is logged.
4. Language.
   - Call `salesbrain.DecideLanguage(contactPreferredLanguage(contact), salesbrain.LangID, userText, lastOutboundWasLanguageAsk(lastBody))`.
   - If `Persist != ""`, call `saveContactPreferredLanguage` and `bf.SetSavedBuyerLang`.
   - If `AskChoice`, send `salesbrain.T(TplLanguageChoice, LangID, shop)` with `PathBuyerLanguage` and return `true`.
5. Build `planInput`:
   - Map the catalog to `CatalogFact`. `No` is 1-based in load order. `Stock` is `"tersedia"`/`"habis"` only when `StockTracked`. Description comes from Part D.
   - Map KB to `KBFact`.
   - Fill the TurnState fields:
     - `Greeting`: from Asia/Jakarta time. 05–11 pagi, 11–15 siang, 15–19 sore, otherwise malam.
     - `Now`: `"Sabtu 17 Okt 2026 14:20 WIB"` style, Indonesian day and month names.
     - `OpenOrders`: map statuses as follows:
       - DB `draft` → `StatusDraft`;
       - an unpaid order waiting for payment → `StatusAwaitingPayment`. Inspect the order service status values with `grep -rn "status" order/*.go | grep -i "const\|=\s*\""` and map conservatively. Anything else is ignored.
     - `Stage`: from the shipping JSON.
     - `Ref`: from the existing ref formatter. Find it by grepping `"WB-"`.
     - `RemindAllowed`: from `salesbrain.ReminderAllowed`. `PrevBuyerMessageAt` is the previous contact message in history. `FillingField` is `PendingField != ""`.
     - `Cart`: from `cartFromState`.
     - `PendingField` and `ResumeLine`.
     - `LastBotQuestion`: the last outbound body when it ends with `?`.
     - `UpsellDone`: `st.UpsellShown`.
     - `Misunderstood`: Redis counter `ai:llmfirst:confused:<tenant>:<convo>`.
   - History: map to `HistoryTurn` (contact → buyer, ai/system → shop, human → staff) and drop the current inbound with Part D's `dropTrailingCurrentInbound` equivalent.
   - Topk: when `salesbrain.ChooseContextMode(...) == "topk"`, keep the 12 catalog items with the best `overlapScore` against the message plus every cart item. Keep `kbForLLMPrompt(all, retrieved)` with the existing retrieval.
6. Run `plan := planTurn(ctx, in, s.llmFirstChat(payload.TenantID))`.
   - `llmFirstChat` wraps `llm.RunChat(ctx15s, LLMRegistry(secrets.AnthropicAPIKey, AnthropicConfig{Model: defaultAnthropicModel, MaxTokens: defaultAnthropicMaxToks}), tenantID, llm.FnChat, req)`.
   - Always record the usage of the calls made, even on fallback: `RecordAITokens` and the `ai_token` event with input + output + thoughts.
   - If `plan.Fallback`, log `rlog.Warn("llm_first fallback", why, attempts, violations)` and return `(false, nil)`.
7. Side effects by `plan.Decision.Intent`. These are the first writes, so from here on errors are returned, not swallowed.
   - `order`, `amend`: if `plan.CartChanged`, `setOrderState(stateWithCart(...))`. For an empty cart, `clearOrderState`.
   - `field_answer`: apply `applyFieldAnswer` to the current state and save it. If there are problems, replace the reply with the model reply plus the problems plus the resume line.
     - When the step becomes `confirm`, append the full summary with the address and `ResumeLineFor("confirm")`.
   - `confirm`: only when the step is `confirm` and `ShippingComplete()`. Persist the order through the SAME function the legacy order-flow completion uses (find the persist callback passed to `AdvanceOrderFlow` in `ai/order_flow_handler.go`). Append the order ref line in the buyer language.
   - `cancel`: cancel the open draft through the same function the legacy `order_cancel` branch uses, and `clearOrderState`.
     - For a paid order, use `TplCancelPaidOrder` and call `notifyOwner(…, "ai.handoff", …)`.
     - The reply is the template text, which is deterministic.
   - `continue_order`: rebuild the state from the newest open order (items JSON → `stateWithCart`, step from stage) and save it. The reply is the model reply plus the summary plus the resume line.
   - `new_order`: `clearOrderState` (keep the draft row). If the model reply lacks the ref, append `TplNewOrderParked(ref)`.
   - `status`: return `s.handleCustomerOrderStatus(ctx, ts, payload.TenantSchema, payload, convo, channel, contact, userText, nil)` (it sends its own reply).
   - Any intent with `Handoff.Level == takeover`: after sending, `pauseAI(ctx, ts, convo.ID, HandoffReasonHumanRequest)` and `notifyOwner(…, "ai.handoff", …)`.
   - `Handoff.Level == note`: `notifyOwner` only.
   - Reminder bookkeeping: if `RemindAllowed` and the reply contains the newest open order ref, update the Redis reminder state (`lastAt = now`, `count[ref]++`, TTL 72h).
   - Confusion counter: increment on intent `other`, reset otherwise, TTL 24h.
   - Upsell: if the reply mentions a catalog product not in the cart and the intent is order or amend, set `UpsellShown = true` on the saved state.
8. Send with `s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, plan.Reply, "ai", payload.InboundMessageID, meta)`.
   - `meta`: `AiReplyMeta{Reason: reasonAIGenerated, Path: PathLLMFirst, Model: plan.Usage.Model, LLMUsed: true, Pipeline: "llm_first", Intent: string(plan.Decision.Intent), Lang: string(plan.Lang), Attempts: plan.Attempts, RawOutput: plan.RawOutput}`.
   - Call `meta.LogAndRecord(...)` with the tokens, then `usage.TrackAIExchange` and the `ai_conversation` event exactly like the legacy LLM path (≈:819-843).
   - Return `(true, nil)` on success.
9. `shadowLLMFirst` runs steps 3–6 with no writes at all: no language persistence, no state, no sends. It logs one line, `rlog.Info("llm_first shadow", "intent", …, "lang", …, "fallback", …, "why", …, "attempts", …, "replyPreview", previewText(plan.Reply, 200), "inTok", …, "outTok", …)`. It may record `ai_token` usage, because shadow costs real tokens. Mark the activity purpose `"llm_first_shadow"`.

- [ ] **Step 1: Failing tests.** Cover pure helpers extracted from the shell:
  - `greetingFor(time.Time) string` with the WIB boundaries.
  - `formatNowID(time.Time) string` → `"Sabtu 17 Okt 2026 14:20 WIB"` for 2026-10-17 07:20 UTC.
  - `historyTurnsFrom([]dbMessage, currentText string) []salesbrain.HistoryTurn`: role mapping, current message dropped.
  - `mapOpenOrders([]persistedOrder, now)`: draft is kept, paid/shipped are dropped.
  - `topKCatalog(...)` keeps cart items.
- [ ] **Step 2: Run** `encore test ./ai/ -run 'LLMFirst' -count=1`. Expected: FAIL.
- [ ] **Step 3: Implement** the shell and the hook as above.
- [ ] **Step 4: Run**
  - `encore test ./ai/ -run 'LLMFirst|Plan' -count=1`
  - the full `encore test ./ai/ -count=1` once (it may take several minutes; report its result)
  - `go test ./internal/... -count=1`
  - `go build ./... && go vet ./ai/ ./flag/`

  Expected: PASS. Any legacy test failure must be explained: a flag-off tenant must behave as before, so legacy tests should not change in this task.
- [ ] **Step 5: Commit** `feat(ai): pipeline LLM-first pelayan toko di belakang flag per tenant`.

---

### Task 6: Developer documentation

**Files:**
- Create: `docs/LLM_FIRST_PIPELINE.md`
- Create: `docs-development-shipped/20261017_pelayan-toko-llm-first.md` (follow the existing file style in that folder)

Content: the flags and how to set them (`POST/PATCH /api/v1/flags` as super_admin, with a curl example and placeholder tokens only), shadow → canary → rollout steps, kill switch, the metadata fields and the SQL to measure them, the cost per message estimate (§2 of the spec), the env notes (Gemini on Cloud Run only; Encore Cloud falls back to Anthropic), and how to roll back.

- [ ] **Step 1:** Write both files.
- [ ] **Step 2:** Commit `docs: panduan pipeline LLM-first pelayan toko`.
