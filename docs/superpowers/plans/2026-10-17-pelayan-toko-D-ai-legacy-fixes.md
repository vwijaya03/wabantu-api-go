# Pelayan Toko — Part D: legacy fixes in package `ai` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the incident bugs in package `ai` (`ProcessAutoReply` and its helpers). These fixes apply to every tenant through the legacy path, and that path is also the fail-open fallback of the new LLM-first pipeline.

**Architecture:** Make targeted edits inside `ai/`. Put new behaviour in small helpers that can be unit-tested. Reuse `internal/salesbrain` for templates, language detection and the amount allow-list. Keep `ProcessAutoReply` changes minimal and local.

**Tech Stack:** Go 1.26, Encore. Package `ai` tests run ONLY with `encore test ./ai/ -run '<Regex>' -count=1` (Docker is up; `encore` is `/opt/homebrew/bin/encore`; the first run may take minutes). Plain `go test ./ai/` panics by design. Use `go build ./... && go vet ./ai/` for quick compile checks. `go test ./internal/buyerflow/ ./internal/salesbrain/` stays plain.

Spec: `docs/superpowers/specs/2026-10-17-pelayan-toko-llm-first-design.md` §4.4, §5.

## Global Constraints

- Worktree `/Users/viko/Documents/WABantu/.worktrees/api-go-pt-d-ai`, branch `feat/pt-d-ai` (from `feat/pelayan-toko-llm-first`).
- **File ownership.** Work in `ai/*`, `internal/salesbrain/templates.go` (add keys only), and these two buyerflow edits from Task 1:
  - `internal/buyerflow/types.go`: add `CatalogItem.Description` only.
  - `internal/buyerflow/catalog_reply.go`: `BuildCatalogContext` only.
  - Parallel agents own other buyerflow files. Do not touch `OrderState` and do not reference `OrderState.UpsellShown`, which belongs to another branch.
- **No schema changes, no new secrets or env vars, and no new `//encore:api` endpoints.** New endpoints would break `internal/apiregistry` snapshots.
- **Cross-package helpers you may call:**
  - `tenantaccess.ListOwnerAccountIDs(ctx, tenantID) ([]string, error)`;
  - `notification.CreateForAccounts(ctx, accountIDs, kind, title, body, linkPath string) error`. It has no dedupe, so dedupe with Redis `s.rdb.SetNX(ctx, key, "1", time.Hour)`.
- **Language of any canned text.** Use `salesbrain.T(key, lang, ...)`, with `lang := salesbrain.ParseLang(ResolveBuyerLanguage(userText, contactPreferredLanguage(contact)))`. If that is empty, use `salesbrain.LangID`. Never add an Indonesian-only string.
- **Existing tests.** Never delete tests. Update an expectation only when it encodes the buggy behaviour, and record it in the commit message as `Tes diperbarui: <file>:<Test> (<why>)`. If `encore test` cannot run, report the exact error and still make `go build ./... && go vet ./ai/` pass.
- **Commits.** Messages in Indonesian with a conventional prefix and the trailer `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Use `set -o pipefail` when piping test output. Never print secrets or touch `.env*` files. Do not push.

---

### Task 1: Product descriptions reach the LLM, tools and catalog search (B1, S10)

**Files:**
- Modify: `internal/buyerflow/types.go`: add `Description string` to `CatalogItem` after `SellUnit`.
- Modify: `internal/buyerflow/catalog_reply.go` `BuildCatalogContext` (≈:851-873).
- Modify: `ai/order_catalog_db.go` `loadActiveCatalog` (SELECT ≈:93-98) and the `dbCatalogItem` struct/scan.
- Modify: `ai/kb_fetch.go` `loadCatalogItemsByIDs` (SELECT ≈:45-50).
- Modify: the `dbCatalogItem → bf.CatalogItem` mapping. Find it with `grep -n "bf.CatalogItem{" ai/*.go`.
- Modify: `ai/catalog_tools.go`:
  - `catalogToolItem` (≈:15-24): add `Description string \`json:"description,omitempty"\``.
  - `formatCatalogToolItem`;
  - `searchCatalogItems` (≈:84-100);
  - the not-found results (≈:50, :63).
- Test: `internal/buyerflow/catalog_context_description_test.go` (new, plain go test) and `ai/catalog_tools_description_test.go` (new, encore test).

- [ ] **Step 1: Failing tests.** Create `internal/buyerflow/catalog_context_description_test.go`:
```go
package buyerflow

import (
	"strings"
	"testing"
)

func TestBuildCatalogContextIncludesDescription(t *testing.T) {
	cat := []CatalogItem{{ID: "tuna-100", ExternalCode: "YELLOW_TAIL_TUNA_PORTION_100_GRAM", Name: "Yellow Tail Tuna Portion 100 Gram", SellPrice: 350000, SellUnit: "pcs",
		Description: "Premium cut of frozen yellow tail tuna steak, 100 grams per portion. Sold frozen and vacuum-sealed to lock in freshness and premium quality. " + strings.Repeat("x", 300)}}
	ctx := BuildCatalogContext(cat)
	if !strings.Contains(ctx, "Sold frozen and vacuum-sealed") {
		t.Fatalf("description missing from LLM catalog context:\n%s", ctx)
	}
	if strings.Contains(ctx, strings.Repeat("x", 200)) {
		t.Fatal("description must be truncated (≤160 runes)")
	}
}
```
In `ai/catalog_tools_description_test.go`, use the real names `searchCatalogItems` and `formatCatalogToolItem` after reading them. Assert:
1. With the omah steak catalog, where the tuna item's description contains "frozen yellow tail tuna steak", `searchCatalogItems("tuna steak", catalog, 3)[0]` is the Yellow Tail Tuna item.
2. `get_product` and `search_catalog` results include `"description"`.
3. A miss returns JSON with `"found":false` and NO prose `message` field, and contains no Indonesian text.

Build the `dbCatalogItem` values inline with the same five products and prices as `internal/buyerflow/fixtures_omah_steak.go`.

- [ ] **Step 2: Run** `go test ./internal/buyerflow/ -run BuildCatalogContextIncludesDescription -count=1` and `encore test ./ai/ -run 'CatalogTool.*Description|SearchCatalog' -count=1`. Expected: FAIL.

- [ ] **Step 3: Implement**
- `BuildCatalogContext`: after `kode internal: CODE`, append ` | ` + `sanitizeCatalogContextField(truncate(it.Description, 160))` when the description is non-empty. Use the package's UTF-8-safe truncation. If none exists, slice runes and add `…`.
- SQL: add `COALESCE(description, '')` to both SELECTs and scan it into a new `dbCatalogItem.Description string`. Map it to `bf.CatalogItem.Description`.
- Tools:
  - `formatCatalogToolItem` copies the description, truncated to 300 runes.
  - `searchCatalogItems` computes `descScore := overlapScore(tokens, tokenize(strings.ToLower(it.Description)))`. Use `score = max(score, 0.6*descScore)` and keep the existing name/code bonuses.
  - Not-found results become exactly `{"found":false,"items":[]}` (search) and `{"found":false}` (get_product).
  - Keep `isGenericCatalogNotFound` recognising the old Indonesian sentence for backward safety.

- [ ] **Step 4: Run** both test commands plus `set -o pipefail; go test ./internal/buyerflow/ -count=1 2>&1 | tail -20` and `go build ./... && go vet ./ai/`. Expected: PASS.

- [ ] **Step 5: Commit** `fix(ai): deskripsi produk masuk ke konteks LLM, tool, dan pencarian katalog`.

---

### Task 2: Grounding never replaces a reply with canned Indonesian (B2, S16, S18, S21)

**Files:**
- Modify: `internal/salesbrain/templates.go`. Add key `TplCheckWithTeam`:
  - ID: `"Sebentar ya Kak, aku cek dulu ke tim toko dan kabari di chat ini."`
  - EN: `"Let me double-check that with the shop team and get back to you here."`
  - Add it to the existing tests' expectations if a count is asserted.
- Modify: `ai/catalog_validator.go`:
  - `validateReplyAgainstCatalog`;
  - `groundLLMReply` (≈:148-194), including the payment fallback ≈:190-191 and the final fallback ≈:193.
- Modify: `ai/autoreply.go`, the call site ≈:807. Pass the KB entries, the current order state and the buyer language. Record the reason in the metadata.
- Modify: `ai/reply_meta.go` `AiReplyMeta`: add `GroundReason string \`json:"groundReason,omitempty"\``.
- Test: `ai/catalog_validator_grounding_test.go` (new).

- [ ] **Step 1: Failing tests** (encore test). Write tests that call `groundLLMReply` and `validateReplyAgainstCatalog` with the omah steak catalog:
  - A reply containing `Total: Rp875.000` while the current cart lines are Tuna 350000 + Salmon 75000 + Beef 450000 is NOT grounded (`grounded == false`).
  - A reply quoting `Rp150.000` for "2x Salmon Steak 100 Gram" is NOT grounded.
  - A reply quoting an invented `Rp325.000` IS grounded. For an English buyer the final text is the EN `TplCheckWithTeam` text and never contains "Saya belum menemukan". For an Indonesian buyer it is the ID template.
  - The returned reason is non-empty when grounded.

- [ ] **Step 2: Run** `encore test ./ai/ -run 'Grounding' -count=1`. Expected: FAIL.

- [ ] **Step 3: Implement**
- Extend the signatures so the validator can build an allow-list with `salesbrain.BuildFacts`:
  - `groundLLMReply(reply, userText string, lang salesbrain.Lang, profile, catalog, kb []dbKBEntry, cart *orderState, history, catalogFn)`;
  - `validateReplyAgainstCatalog(..., facts salesbrain.Facts)`.
  - `FactsInput.Catalog`: one `salesbrain.CatalogFact{No: i+1, Name, Price: int64(SellPrice), Code: ExternalCode}` per catalog item.
  - `KB`: one `salesbrain.KBFact` per entry.
  - `Cart`: `salesbrain.OrderLine{Name, Qty, UnitPrice}` from `cart.Items` (fall back to the single-item fields when `Items` is empty), and `CartSubtotal` as the sum.
- Before any price-based rejection, if every amount in the reply is in `facts.Amounts`, accept the reply. Use `salesbrain.Validate(salesbrain.Decision{Reply: reply}, facts, lang)`, keep only `VAmount`, and treat "no `VAmount` violations" as accept.
- Replace both hard-coded Indonesian fallbacks with `salesbrain.T(salesbrain.TplCheckWithTeam, lang)`. For the payment fallback, first try the KB payment answer (`TryPaymentFAQAnswer(userText, kbEntries)` via the bridge) and use the template only if that fails.
- At the call site, set `out.GroundReason = groundReason` when grounded.

- [ ] **Step 4: Run** `encore test ./ai/ -run 'Grounding|CatalogValidator|Validate' -count=1`, `go test ./internal/salesbrain/ -count=1`, and `go build ./... && go vet ./ai/`. Expected: PASS. Apply the update-old-expectations rule from the Global Constraints.

- [ ] **Step 5: Commit** `fix(ai): grounding tidak lagi mengganti balasan dengan kalimat baku Indonesia`.

---

### Task 3: No pause on out-of-scope or system errors; honest hard handoff with auto-resume and owner notification (B5, S7, S15, S20, S34)

**Files:**
- Create: `ai/handoff_v2.go`
- Modify: `ai/autoreply.go`:
  - early gate `if !convo.AIHandled` (≈:183);
  - human-escalation branch (≈:344);
  - `classifier.Label == "out_of_scope"` branch (≈:624);
  - LLM error branch (≈:791-801, which calls `handleAiNoAnswerHandoff`);
  - `FallbackAutoReply` (≈:849-878);
  - `handleAiNoAnswerHandoff` (≈:1608-1627).
- Modify: the `dbConversation` struct/loader, if `AIPausedAt` / `HandoffReason` are not already scanned (they are loaded according to the code map; verify).
- Test: `ai/handoff_v2_test.go` (new).

**Interfaces (produce exactly these):**
```go
const (
	HandoffReasonHumanRequest = "human_request" // buyer explicitly asked for a person
	HandoffReasonAIFallback   = "ai_fallback"   // legacy name kept for FallbackAutoReply rows already in DB
	AutoResumeAfter           = 2 * time.Hour
)

// aiInitiatedPauseReasons — only pauses the AI created itself may auto-resume.
// Staff takeover from the inbox must never auto-resume.
func isAIInitiatedPause(reason string) bool

// shouldAutoResume: AI-initiated pause, paused at least AutoResumeAfter ago, no staff reply since.
func shouldAutoResume(reason string, pausedAt *time.Time, now time.Time, staffRepliedSince bool) bool

// notifyOwner sends one in-app notification to the tenant owners, deduped per kind+conversation
// for one hour via Redis. Errors are logged, never returned to the reply path.
func (s *AutoReplyService) notifyOwner(ctx context.Context, tenantID, convoID, kind, title, body string)
```
`isAIInitiatedPause` returns true for `AiNoAnswerPauseReason` ("ai_no_answer"), `HandoffReasonHumanRequest`, `HandoffReasonAIFallback`, and the reason string `FallbackAutoReply` writes today. Read the code and include its literal. It returns false for `""` and for any reason the inbox takeover writes. Find those with `grep -rn "handoff_reason" inbox/`.

- [ ] **Step 1: Failing test** `ai/handoff_v2_test.go`:
```go
package ai

import (
	"testing"
	"time"
)

func TestShouldAutoResume(t *testing.T) {
	now := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	old := now.Add(-3 * time.Hour)
	recent := now.Add(-30 * time.Minute)
	cases := []struct {
		name        string
		reason      string
		pausedAt    *time.Time
		staffReplied bool
		want        bool
	}{
		{"ai no answer, old", AiNoAnswerPauseReason, &old, false, true},
		{"human request, old", HandoffReasonHumanRequest, &old, false, true},
		{"too recent", HandoffReasonHumanRequest, &recent, false, false},
		{"staff replied", HandoffReasonHumanRequest, &old, true, false},
		{"staff takeover never", "staff_takeover", &old, false, false},
		{"empty reason never", "", &old, false, false},
		{"no timestamp", AiNoAnswerPauseReason, nil, false, false},
	}
	for _, c := range cases {
		if got := shouldAutoResume(c.reason, c.pausedAt, now, c.staffReplied); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
```
Replace `"staff_takeover"` with the real inbox takeover reason literal if one exists.

- [ ] **Step 2: Run** `encore test ./ai/ -run TestShouldAutoResume -count=1`. Expected: FAIL (undefined).

- [ ] **Step 3: Implement**
- `ai/handoff_v2.go`:
  - The constants and functions above.
  - `notifyOwner` uses `tenantaccess.ListOwnerAccountIDs` and `notification.CreateForAccounts(ctx, ids, kind, title, body, link)`.
  - `kind` is `"ai.handoff"` or `"ai.system_error"`.
  - `link` is the inbox deep link to the conversation. Check the web-frontend inbox route for the query parameter name: `grep -rn "searchParams" /Users/viko/Documents/WABantu/web-frontend/app/\(dashboard\)/dashboard/inbox`. If there is no param, use `/dashboard/inbox`.
  - Dedupe key: `fmt.Sprintf("ai:notify:%s:%s:%s", kind, tenantID, convoID)` with `SetNX` and a 1h TTL. If `s.rdb == nil`, send without dedupe.
- Early gate. When `!convo.AIHandled`:
  - Compute `staffRepliedSince` with one query: `SELECT EXISTS (SELECT 1 FROM <message> WHERE conversation_id=$1 AND direction='out' AND author='human' AND created_at > $2)`, using `ts.T("message")`.
  - If `shouldAutoResume(...)`, run `UPDATE <conversation> SET ai_handled = true, ai_paused_at = NULL, handoff_reason = NULL, updated_at = now() WHERE id = $1`. Send `salesbrain.T(salesbrain.TplHandoffResumed, lang)` as an `ai` message (`metaNoLLM(reasonNonQuestion, PathHumanHandoff)`), then continue processing this inbound normally (`convo.AIHandled = true`).
  - Otherwise keep the current `ErrAIHandoffPaused` behaviour.
- Out-of-scope branch. Do not hand off or pause. Remove the early return so the turn continues to the FAQ-cache, FAQ-direct and LLM steps exactly like an in-scope question. Keep the classifier label in the activity metadata for observability.
- LLM error branch, `handleAiNoAnswerHandoff` and `FallbackAutoReply`:
  - Send `salesbrain.T(salesbrain.TplSystemBusy, lang, shopName)`.
  - Call `s.notifyOwner(..., "ai.system_error", ...)` with a 2-line summary: buyer message preview via `previewText(userText, 160)`, and the reason.
  - Do NOT call `pauseAI`. Keep the existing metadata paths so dashboards still count them.
- Human-escalation branch:
  - Reply `salesbrain.T(salesbrain.TplHandoffTakeover, lang, shopName, salesbrain.OpeningHoursSentence(strOrEmpty(profile.OpeningHours), lang))`.
  - Pause with `pauseAI(ctx, ts, convo.ID, HandoffReasonHumanRequest)`.
  - Call `notifyOwner(..., "ai.handoff", ...)`.

- [ ] **Step 4: Run**
- `encore test ./ai/ -run 'ShouldAutoResume|Handoff|NoAnswer|Fallback|Escalat' -count=1`
- `go build ./... && go vet ./ai/`

Expected: PASS. Existing tests that assert a pause after out-of-scope or no-answer encode the bug, so update them per the rule.

- [ ] **Step 5: Commit** `fix(ai): tidak ada pause tanpa batas; handoff jujur dengan resume otomatis dan notifikasi owner`.

---

### Task 4: Whole-order cancel is evaluated before amend; cancel never adds lines from history (B6, S29)

**Files:**
- Modify: `ai/autoreply.go`, the branches ≈:358 (amend) and ≈:362 (cancel).
- Modify: `ai/order_amend.go`, the `ExtractAmendLinesFromHistory` call ≈:512.
- Test: `ai/cancel_precedence_test.go` (new).

**Interfaces:** `func isWholeOrderCancelText(text string) bool { return IsOrderCancelRequest(text) && !IsCartLineCorrectionIntent(text) && !IsNegatedFullOrderCancel(text) }`. Also `func textHasCancelOrRemoveVerb(text string) bool` (cancel, batal, batalkan, batalin, hapus, remove, delete, gak jadi, ga jadi, nggak jadi).

- [ ] **Step 1: Failing test.** It is a table test of both helpers:
  - `isWholeOrderCancelText` is true for "please cancel my previous order" and "batalkan pesanan", and false for "cancel the tuna" and "bukan batal semua, cuma tuna".
  - `textHasCancelOrRemoveVerb` is true for "please cancel my previous order" and "hapus salmon".

  Note: "please cancel my previous order" only becomes a whole-order cancel once Part C (another branch) is merged. Until then, assert only `textHasCancelOrRemoveVerb` for that phrase and put the `isWholeOrderCancelText` English cases behind `t.Skip("needs Part C buyerflow phrases")` when `IsCartLineCorrectionIntent` still returns true. The integration step removes the skip.
- [ ] **Step 2: Run** `encore test ./ai/ -run CancelPrecedence -count=1`. Expected: FAIL (undefined).
- [ ] **Step 3: Implement**
  - Move the whole-order cancel branch above the amend branch, using `isWholeOrderCancelText`.
  - In `handleOrderAmend`, skip `ExtractAmendLinesFromHistory` when `textHasCancelOrRemoveVerb(userText)`.
- [ ] **Step 4: Run** the tests, `go build ./... && go vet ./ai/`. Expected: PASS.
- [ ] **Step 5: Commit** `fix(ai): batal seluruh pesanan didahulukan dari ubah pesanan`.

---

### Task 5: Never send an empty WhatsApp body (B7, S32)

**Files:**
- Modify: `ai/autoreply.go` `sendAiMessage` (≈:1470): add the guard as the very first statement after `meta = metaForSend(...)`.
- Test: `ai/send_empty_guard_test.go` (new).

**Interfaces:** `var errEmptyReply = errors.New("ai: refusing to send an empty reply")`.

- [ ] **Step 1: Failing test.**
  - Override `metaSendText` and `simulatorOutbound` as in `ai/simulator_route_test.go:107-112`, with `t.Cleanup` restore.
  - Call `(&AutoReplyService{}).sendAiMessage(ctx, nil, "t", &dbConversation{ID: "c"}, &dbChannel{Provider: "meta_cloud"}, &dbContact{}, "   ", "ai", "in-1", AiReplyMeta{})`.
  - Assert `errors.Is(err, errEmptyReply)` and that the overridden `metaSendText` was never called.
- [ ] **Step 2: Run** `encore test ./ai/ -run SendEmpty -count=1`. Expected: FAIL.
- [ ] **Step 3: Implement**
```go
	if strings.TrimSpace(text) == "" {
		rlog.Error("AI job: refusing to send empty reply", "path", meta.Path, "inboundId", inboundMessageID)
		return errEmptyReply
	}
```
- [ ] **Step 4: Run** the test, `go build ./... && go vet ./ai/`. Expected: PASS.
- [ ] **Step 5: Commit** `fix(ai): jangan pernah mengirim balasan kosong`.

---

### Task 6: Structured order lists only break an active checkout and merge into the cart (B8, S18, S31)

**Files:**
- Modify: `ai/autoreply.go`, the gates ≈:535 and ≈:649 that use `isStructuredOrderCheckoutBreak(userText)`.
- Modify: `ai/order_flow_handler.go` ≈:92-95 (`IsStructuredOrderList` clears the state).
- Test: `ai/structured_order_gate_test.go` (new).

**Interfaces:** `func structuredListBreaksCheckout(text string, st *orderState) bool { return st != nil && len(st.Items)+boolToInt(st.ProductName != "") > 0 && isStructuredOrderCheckoutBreak(text) }`. Use a simple length check that fits the real `orderState` fields.

- [ ] **Step 1: Failing test.**
  - With `st == nil`, the multi-line text `"I would like to order\n1 of Australian beef 500grams\n1 of salmon steak 100gr\n1 of yellow tail tuna"` must NOT break, so it reaches the order flow.
  - With an active cart it still breaks, as before.
- [ ] **Step 2: Run.** Expected: FAIL (undefined).
- [ ] **Step 3: Implement**
  - Use `structuredListBreaksCheckout(userText, st)` at both gates. Load `st` from `s.getOrderState` if it is not already in scope at that point; reuse the variable if it is.
  - In `order_flow_handler.go`, when `IsStructuredOrderList(userText)` and a state exists and `!IsExplicitNewOrderStart(userText)`:
    - do NOT `clearOrderState`;
    - parse the lines as today and merge them into the existing state with `MergeOrderLines(existing.Items, newLines)` (use the bf bridge alias, or add one in `ai/buyerflow_bridge.go`);
    - keep the existing recipient/address fields.
  - Clear only for an explicit new-order start.
- [ ] **Step 4: Run** the tests, `go build ./... && go vet ./ai/`. Expected: PASS.
- [ ] **Step 5: Commit** `fix(ai): daftar pesanan multi-baris digabung ke keranjang, tidak dibuang`.

---

### Task 7: LLM context hygiene: no duplicate message, merged hints, full small KB (O5, O6)

**Files:**
- Modify: `ai/anthropic_messages.go` `buildSalesMessages` (≈:112-119).
- Modify: `ai/anthropic_tools.go`: the TaskHint replacement ≈:77 and ≈:237, and `salesMessagesNeutral` ≈:249.
- Modify: `ai/autoreply.go`, where `kbHybrid` is chosen for the prompt (≈:700-760).
- Modify: `ai/prompt.go` `BuildKnowledgeContext` (≈:83).
- Test: `ai/llm_context_hygiene_test.go` (new).

**Interfaces:** `func dropTrailingCurrentInbound(history []HistoryMessage, userText string) []HistoryMessage`. It removes the last element when it is a contact message whose trimmed body equals the trimmed `userText`.

- [ ] **Step 1: Failing tests**
  - (a) `salesMessagesNeutral(SalesReplyRequest{History: [..., {Author:"contact", Body:"Is it frozen?"}], UserText: "Is it frozen?"})` contains "Is it frozen?" exactly once across all messages.
  - (b) `salesSystemText(SalesReplyRequest{TaskHint: "Reply in English."})` contains both "Reply in English." and the catalog-tools hint text (`catalogToolsTaskHint`).
  - (c) `BuildKnowledgeContext` with 1 entry does not contain "(belum ada)".
  - (d) The KB-for-prompt selector `kbForLLMPrompt(all []dbKBEntry, retrieved []dbKBEntry) []dbKBEntry` returns `all` (active rows) when `len(all) <= 30`, otherwise `retrieved`.
- [ ] **Step 2: Run.** Expected: FAIL.
- [ ] **Step 3: Implement**
  - Apply `dropTrailingCurrentInbound` in both message builders before appending `UserText`.
  - Replace `if TaskHint == "" { TaskHint = catalogToolsTaskHint }` with `TaskHint = strings.TrimSpace(TaskHint + "\n" + catalogToolsTaskHint)` in both places.
  - Add `kbForLLMPrompt` and use it where the prompt KB is built.
  - `BuildKnowledgeContext` keeps "(belum ada)" only for a truly empty list (it already does). Make sure the selector never hands it an empty list when KB rows exist.
- [ ] **Step 4: Run** `encore test ./ai/ -run 'Hygiene|SalesMessages|KnowledgeContext' -count=1`, `go build ./... && go vet ./ai/`. Expected: PASS.
- [ ] **Step 5: Commit** `fix(ai): konteks LLM tanpa pesan ganda, hint digabung, FAQ kecil dikirim penuh`.

---

### Task 8: The language question is answered only when it was asked (B3 integration, S1)

**Files:**
- Modify: `ai/buyer_language.go` `handleBuyerLanguageRouting` (≈:55-101).
- Modify: `ai/autoreply.go`. If needed, pass the last outbound body. Add a tiny loader `lastAIOutboundBody(ctx, ts, convoID) (string, error)`: `SELECT body FROM <message> WHERE conversation_id=$1 AND direction='out' ORDER BY created_at DESC LIMIT 1`.
- Test: `ai/buyer_language_ask_test.go` (new).

**Interfaces:** `func lastOutboundWasLanguageAsk(body string) bool`. It is true when `body` contains "Bahasa Indonesia atau English" (the text of `LanguagePreferenceAskReply`) or the `salesbrain.TplLanguageChoice` text.

- [ ] **Step 1: Failing test.**
  - `lastOutboundWasLanguageAsk(LanguagePreferenceAskReply())` is true.
  - `lastOutboundWasLanguageAsk("How many pcs? (e.g. 1 pc)")` is false.
- [ ] **Step 2: Run.** Expected: FAIL.
- [ ] **Step 3: Implement.**
  - In `handleBuyerLanguageRouting`, only evaluate `ParseLanguagePreferenceAnswer` when `lastOutboundWasLanguageAsk(lastBody)`. This stops a quantity answer like "1" from switching the language.
  - Keep explicit switches (`ParseExplicitLanguageSwitch`) always active.
  - Confirmation text: replace `bf.LanguageSwitchConfirmReply(lang)` with `salesbrain.T(salesbrain.TplSwitchConfirm, salesbrain.ParseLang(lang))`.
- [ ] **Step 4: Run** the tests, `go build ./... && go vet ./ai/`. Expected: PASS.
- [ ] **Step 5: Commit** `fix(ai): jawaban pilihan bahasa hanya dibaca setelah bot bertanya`.
