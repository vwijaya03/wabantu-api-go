package ai

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"encore.dev/rlog"

	appflag "encore.app/wabantu/flag"
	"encore.app/wabantu/internal/salesbrain"
	"encore.app/wabantu/shared/llm"
	"encore.app/wabantu/usage"
)

// LLM-first pipeline (spec §3.1): the thin I/O shell around planTurn.
//
// Per flag mode of the tenant (flag.LLMFirstMode):
//
//	off     tryLLMFirst returns at once. Nothing else runs, so the legacy flow is untouched.
//	shadow  planTurn runs in a goroutine and its result is logged. Nothing is written, sent or paused.
//	on      this file answers the buyer: loads, language lock, planTurn, side effects, send.
//
// Fail-open: any problem before the first write returns (false, nil) and the legacy flow handles
// the turn. A pipeline error never pauses the AI.

const (
	lfPipeline      = "llm_first"
	lfShadowPurpose = "llm_first_shadow" // usage activity purpose of a shadow run

	lfCallTimeout   = 15 * time.Second // per LLM call (planTurn makes at most two)
	lfShadowTimeout = 20 * time.Second // whole shadow run

	lfHistoryLoad = 12 // messages loaded for the prompt (the prompt shows the last six)
	lfKBLoad      = 50 // FAQ rows loaded; the prompt gets them all when they fit the budget
	lfTopKRows    = 12 // catalog rows kept when the catalog does not fit the full-context budget

	lfReplacedConfirmHold = "confirm_fail_closed" // message metadata: the summary was re-sent because the turn failed at the confirm step

	lfRemindTTL   = 72 * time.Hour
	lfConfusedTTL = 24 * time.Hour
)

// Seams (swapped in tests only).
var (
	llmFirstModeFn  = appflag.LLMFirstMode
	llmFirstChatFor = lfNewChat
	llmFirstNow     = time.Now
	llmFirstLoad    = (*AutoReplyService).loadLLMFirst
	// llmFirstTokenSink writes the ai_token_llm_first event; tests capture it.
	llmFirstTokenSink = usage.RecordLLMFirstTokens
)

// lfTurn bundles what ProcessAutoReply already loaded for this inbound message.
type lfTurn struct {
	ts       tenantScopedQuerier
	payload  AiReplyJobPayload
	convo    *dbConversation
	channel  *dbChannel
	contact  *dbContact
	profile  *dbBusinessProfile
	userText string
}

func (t lfTurn) scope() orderAccessScope {
	return orderAccessScope{ConversationID: t.convo.ID, ContactID: t.convo.ContactID}
}

// lfLoaded is everything read for one turn. All of it is read-only.
type lfLoaded struct {
	history  []dbMessage
	lastBody string // body of our newest outbound message
	catalog  []dbCatalogItem
	kbAll    []dbKBEntry
	state    *orderState
	orders   []persistedOrder
	remind   salesbrain.ReminderState
	confused int
}

// tryLLMFirst is the hook ProcessAutoReply calls before its legacy interceptors. It returns
// handled=true when the turn was answered here. (false, nil) hands the turn to the legacy flow.
func (s *AutoReplyService) tryLLMFirst(
	ctx context.Context,
	ts tenantScopedQuerier,
	payload AiReplyJobPayload,
	convo *dbConversation,
	channel *dbChannel,
	contact *dbContact,
	profile *dbBusinessProfile,
	userText string,
) (bool, error) {
	mode := llmFirstModeFn(ctx, payload.TenantID)
	if mode != "on" && mode != "shadow" {
		return false, nil // "off": exactly the legacy behaviour, no further work
	}
	if IsPromptInjectionLikely(userText) {
		return false, nil // the legacy guard answers it; no tokens are spent on it
	}
	t := lfTurn{ts: ts, payload: payload, convo: convo, channel: channel, contact: contact, profile: profile, userText: userText}
	if mode == "shadow" {
		s.startLLMFirstShadow(ctx, t)
		return false, nil
	}
	return s.runLLMFirst(ctx, t)
}

// lfPersistLanguage locks the language salesbrain decided: it writes contact.preferred_language
// and then mirrors the new value in memory, in the contact and in the turn's saved-language stack.
// The legacy templates (when this turn fails open) and the deferred applyBuyerLanguageFromMessage of
// ProcessAutoReply read exactly those two, so neither can undo the decision. A failed write changes
// nothing in memory.
func lfPersistLanguage(ctx context.Context, ts tenantScopedQuerier, contact *dbContact, lang salesbrain.Lang) error {
	if err := saveContactPreferredLanguage(ctx, ts, contact.ID, string(lang)); err != nil {
		return err
	}
	contact.PreferredLanguage = strPtr(string(lang))
	SetSavedBuyerLang(string(lang))
	return nil
}

// runLLMFirst answers the turn in "on" mode.
func (s *AutoReplyService) runLLMFirst(ctx context.Context, t lfTurn) (bool, error) {
	tenantID, schema := t.payload.TenantID, t.payload.TenantSchema

	// 2. Cost gate. Same quota check as the legacy LLM path; the answer is the localized template.
	if ok, reason := usage.CheckAICostLimit(ctx, schema, tenantID); !ok {
		rlog.Warn("llm_first: tenant cost limit reached", "reason", reason, "convoId", t.convo.ID)
		lang := buyerReplyLang(t.userText, t.contact)
		meta := metaNoLLM(reasonOutOfScope, PathCostLimit)
		meta.LogAndRecord(ctx, t.convo.ID, t.payload.InboundMessageID, 0, 0)
		err := s.sendAiMessage(ctx, t.ts, tenantID, t.convo, t.channel, t.contact,
			salesbrain.T(salesbrain.TplQuotaExhausted, lang, shopLabel(t.profile, lang)),
			"system", t.payload.InboundMessageID, meta)
		return err == nil, err
	}

	// 3. Loads (read-only). Any failure hands the turn to the legacy flow.
	ld, err := llmFirstLoad(s, ctx, t)
	if err != nil {
		rlog.Warn("llm_first: load failed, legacy flow takes the turn", "convoId", t.convo.ID, "err", err)
		return false, nil
	}

	// 4. Language. salesbrain.DecideLanguage is the only language authority; the model's own
	// language_switch is never applied. languageAsked is read the way Part D reads it: only the
	// reply to the language question is a language choice (a bare "1" after "How many?" is a qty).
	languageAsked := lastOutboundWasLanguageAsk(ld.lastBody)
	dec := salesbrain.DecideLanguage(contactPreferredLanguage(t.contact), salesbrain.LangID, t.userText, languageAsked)
	if dec.Persist != "" {
		if err := lfPersistLanguage(ctx, t.ts, t.contact, dec.Persist); err != nil {
			rlog.Warn("llm_first: language persist failed, legacy flow takes the turn", "convoId", t.convo.ID, "err", err)
			return false, nil
		}
	}
	if dec.AskChoice {
		meta := metaNoLLM(reasonNonQuestion, PathBuyerLanguage)
		meta.LogAndRecord(ctx, t.convo.ID, t.payload.InboundMessageID, 0, 0)
		err := s.sendAiMessage(ctx, t.ts, tenantID, t.convo, t.channel, t.contact,
			salesbrain.T(salesbrain.TplLanguageChoice, salesbrain.LangID, shopLabel(t.profile, salesbrain.LangID)),
			"ai", t.payload.InboundMessageID, meta)
		return err == nil, err
	}
	locked := contactPreferredLanguage(t.contact) != "" // true after the persist above as well

	// 5-6. Plan the turn.
	in := s.lfPlanInputFor(ctx, t, ld, dec, locked)
	plan := planTurn(ctx, in, llmFirstChatFor(tenantID))

	// Usage is recorded for every call made, a fallback included. TrackAIExchange goes first: it is
	// what tells a new AI session (one ai_conversation event) apart, and RecordAITokens would
	// otherwise create the session without it.
	tokens := lfTokens(plan)
	newSession := false
	if !plan.Fallback {
		_, _, newSession = usage.TrackAIExchange(ctx, tenantID, t.convo.ID)
	}
	if tokens > 0 {
		usage.RecordAITokens(ctx, tenantID, t.convo.ID, tokens)
		_ = usage.RecordEvent(ctx, schema, "ai_token", tokens, nil)
	}
	lfRecordLLMFirstTokens(ctx, t, "on", lfPipeline, plan)
	if plan.Fallback {
		rlog.Warn("llm_first fallback",
			"convoId", t.convo.ID, "why", plan.FallbackWhy, "attempts", plan.Attempts,
			"violations", lfViolationCodes(plan.Violations), "model", plan.Usage.Model, "tokens", tokens)
		if lfAtConfirmWithShipping(ld.state) {
			// Fail-closed: the buyer is looking at the final summary and the pipeline could not read
			// the reply. The legacy flow must not guess whether it was an approval.
			return s.lfHoldConfirm(ctx, t, ld, plan)
		}
		return false, nil
	}
	rlog.Info("llm_first plan",
		"convoId", t.convo.ID, "intent", string(plan.Decision.Intent), "lang", string(plan.Lang),
		"attempts", plan.Attempts, "cartChanged", plan.CartChanged, "unknownItems", len(plan.UnknownItems),
		"model", plan.Usage.Model, "inTok", plan.Usage.InputTokens, "outTok", plan.Usage.OutputTokens,
		"thoughtTok", plan.Usage.ThoughtsTokens, "finish", plan.Usage.FinishReason)

	// "Where is my order?" needs every order of the buyer, which the prompt does not carry: the
	// order-status handler answers (it sends its own reply).
	if plan.Decision.Intent == salesbrain.IntentStatus {
		handled, err := s.handleCustomerOrderStatus(ctx, t.ts, schema, t.payload, t.convo, t.channel, t.contact, t.userText, nil)
		if handled && newSession {
			_ = usage.RecordEvent(ctx, schema, "ai_conversation", 1, nil)
		}
		return handled, err
	}

	// 7. Side effects. These are the first writes of the pipeline: from here on errors are returned.
	out, err := lfApplyIntent(ctx, lfLiveFX{s: s, t: t}, lfApplyIn{
		Plan: plan, UserText: t.userText, Shop: shopLabel(t.profile, plan.Lang),
		OpeningHours: strOrEmpty(t.profile.OpeningHours),
		State:        ld.state, Catalog: ld.catalog, Facts: in.Catalog, Orders: ld.orders, Open: in.State.OpenOrders,
	})
	if err != nil {
		return false, err
	}

	// 8. Send.
	meta := AiReplyMeta{
		Reason: reasonAIGenerated, Path: PathLLMFirst, Model: plan.Usage.Model, LLMUsed: true,
		Pipeline: lfPipeline, Intent: string(plan.Decision.Intent), Lang: string(plan.Lang),
		Attempts: plan.Attempts, RawOutput: plan.RawOutput,
		ReplacedReason: out.ReplacedReason, OrderID: out.OrderID, OrderAction: out.OrderAction,
	}
	meta.LogAndRecord(ctx, t.convo.ID, t.payload.InboundMessageID,
		int(plan.Usage.InputTokens), int(plan.Usage.OutputTokens+plan.Usage.ThoughtsTokens))
	if err := s.sendAiMessage(ctx, t.ts, tenantID, t.convo, t.channel, t.contact, out.Reply, out.Author,
		t.payload.InboundMessageID, meta); err != nil {
		return false, err
	}

	// After the send: what only makes sense once the buyer has the reply.
	var afterErr error
	if out.After != nil {
		afterErr = out.After(ctx)
	}
	if newSession {
		_ = usage.RecordEvent(ctx, schema, "ai_conversation", 1, nil)
	}
	s.lfRecordBookkeeping(ctx, t, ld, in, plan, out.Reply)
	return true, afterErr
}

// lfAtConfirmWithShipping: the checkout waits on the buyer's final OK (confirm step, complete
// shipping data, at least one line).
func lfAtConfirmWithShipping(st *orderState) bool {
	return st != nil && st.Step == lfStepConfirm && st.ShippingComplete() && len(cartFromState(st)) > 0
}

// lfHoldConfirm answers a failed turn that arrives while the checkout waits for the final OK:
// the Redis state and the draft stay untouched, nothing is saved, and the buyer gets the summary
// again with the question for the OK (templates, locked language). The owner is told, because the
// buyer is stuck at the last step until the pipeline works again (the notification is deduped per
// conversation and hour). Not a pause and not an error: the turn is handled.
func (s *AutoReplyService) lfHoldConfirm(ctx context.Context, t lfTurn, ld lfLoaded, plan turnPlan) (bool, error) {
	lang := plan.Lang
	reply := salesbrain.T(salesbrain.TplConfirmHold, lang) + "\n\n" + lfConfirmBlock(ld.state, lang)
	meta := AiReplyMeta{
		Reason: reasonAIGenerated, Path: PathLLMFirst, Model: plan.Usage.Model, LLMUsed: plan.Attempts > 0,
		Pipeline: lfPipeline, Lang: string(lang), Attempts: plan.Attempts, ReplacedReason: lfReplacedConfirmHold,
	}
	meta.LogAndRecord(ctx, t.convo.ID, t.payload.InboundMessageID,
		int(plan.Usage.InputTokens), int(plan.Usage.OutputTokens+plan.Usage.ThoughtsTokens))
	if err := s.sendAiMessage(ctx, t.ts, t.payload.TenantID, t.convo, t.channel, t.contact, reply, "system",
		t.payload.InboundMessageID, meta); err != nil {
		return false, err
	}
	s.notifyOwner(ctx, t.payload.TenantID, t.convo.ID, notifyKindSystemError, notifyTitleSystemError,
		ownerNotificationSummary(t.userText, notifyReasonConfirmHeld))
	return true, nil
}

// lfRecordLLMFirstTokens writes the ai_token_llm_first event of a turn (analytics only: it has no
// plan quota). A failed write is logged, never returned.
func lfRecordLLMFirstTokens(ctx context.Context, t lfTurn, mode, pipeline string, plan turnPlan) {
	convoID := t.convo.ID
	err := llmFirstTokenSink(ctx, t.payload.TenantSchema, usage.LLMFirstTokens{
		Mode: mode, Pipeline: pipeline, TenantID: t.payload.TenantID, ConversationID: convoID,
		InboundMessageID: t.payload.InboundMessageID, Provider: plan.Usage.Provider, Model: plan.Usage.Model,
		Intent: string(plan.Decision.Intent), Fallback: plan.Fallback, Attempts: plan.Attempts,
		InputTokens: int(plan.Usage.InputTokens), OutputTokens: int(plan.Usage.OutputTokens),
		ThoughtsTokens: int(plan.Usage.ThoughtsTokens),
	})
	if err != nil {
		rlog.Warn("llm_first: token event not recorded", "convoId", convoID, "pipeline", pipeline, "err", err)
	}
}

// lfTokens is what a turn cost: input + output + thinking tokens.
func lfTokens(plan turnPlan) int {
	return int(plan.Usage.InputTokens + plan.Usage.OutputTokens + plan.Usage.ThoughtsTokens)
}

// lfRecordBookkeeping stores the two small pieces of per-conversation state that follow a sent
// reply: the open-order reminder (spec D4: at most once per session, twice per order) and the
// "misunderstood in a row" counter. Redis errors are logged, never returned: the buyer has the reply.
func (s *AutoReplyService) lfRecordBookkeeping(ctx context.Context, t lfTurn, ld lfLoaded, in planInput, plan turnPlan, sent string) {
	tenantID, convoID := t.payload.TenantID, t.convo.ID
	if open := in.State.OpenOrders; in.State.RemindAllowed && len(open) > 0 && lfReplyMentionsRef(sent, open[0].Ref) {
		s.saveRemindState(ctx, tenantID, convoID, lfNextRemindState(ld.remind, open[0].Ref, llmFirstNow()))
	}
	next, reset := lfNextConfused(ld.confused, plan.Decision.Intent)
	s.saveConfused(ctx, tenantID, convoID, ld.confused, next, reset)
}

// lfPlanInputFor builds the planTurn input. Only a catalog + FAQ that does not fit the full-context
// budget pays for retrieval (the existing hybrid KB retrieval).
func (s *AutoReplyService) lfPlanInputFor(ctx context.Context, t lfTurn, ld lfLoaded, dec salesbrain.LanguageDecision, locked bool) planInput {
	topK := salesbrain.ChooseContextMode(lfCatalogFacts(ld.catalog), lfKBFacts(ld.kbAll)) == "topk"
	var retrieved []dbKBEntry
	if topK {
		retrieved, _, _ = s.retrieveKBHybrid(ctx, t.payload.TenantID, t.payload.TenantSchema, t.userText, t.ts, ld.kbAll)
	}
	return lfBuildPlanInput(lfInputs{
		Profile: t.profile, Catalog: ld.catalog, KBAll: ld.kbAll, KBRetrieved: retrieved, TopK: topK,
		History: ld.history, LastBody: ld.lastBody, State: ld.state, Orders: ld.orders,
		Remind: ld.remind, Confused: ld.confused, UserText: t.userText, Now: llmFirstNow(),
		Lang: dec, LangLocked: locked,
	})
}

// loadLLMFirst reads everything the turn needs. It writes nothing, so the shadow mode shares it.
func (s *AutoReplyService) loadLLMFirst(ctx context.Context, t lfTurn) (lfLoaded, error) {
	var (
		ld  lfLoaded
		err error
	)
	tenantID, convoID := t.payload.TenantID, t.convo.ID
	if ld.history, err = loadHistory(ctx, t.ts, convoID, lfHistoryLoad); err != nil {
		return ld, fmt.Errorf("history: %w", err)
	}
	if ld.lastBody, err = lastAIOutboundBody(ctx, t.ts, convoID); err != nil {
		return ld, fmt.Errorf("last outbound: %w", err)
	}
	// Catalog: the same functions, in the same order, as the legacy flow before its LLM call.
	if ld.catalog, err = loadActiveCatalog(ctx, t.ts, defaultCatalogLoadLimit); err != nil {
		return ld, fmt.Errorf("catalog: %w", err)
	}
	enrichCatalogStock(ctx, t.ts, ld.catalog)
	applyContactTierPrices(ctx, t.ts, ld.catalog, t.contact.ID)
	if ld.kbAll, err = loadKBEntries(ctx, t.ts, lfKBLoad); err != nil {
		return ld, fmt.Errorf("knowledge base: %w", err)
	}
	if ld.state, err = s.getOrderState(ctx, tenantID, convoID); err != nil {
		return ld, fmt.Errorf("order state: %w", err)
	}
	if ld.orders, err = loadActiveOrdersForContact(ctx, t.ts, t.payload.TenantSchema, t.scope()); err != nil {
		return ld, fmt.Errorf("orders: %w", err)
	}
	if ld.remind, err = s.loadRemindState(ctx, tenantID, convoID); err != nil {
		return ld, fmt.Errorf("reminder state: %w", err)
	}
	if ld.confused, err = s.loadConfused(ctx, tenantID, convoID); err != nil {
		return ld, fmt.Errorf("confusion counter: %w", err)
	}
	return ld, nil
}

// ── LLM call ─────────────────────────────────────────────────────────────────

// lfAnthropicConfig is the legacy-client config behind the registry of the pipeline: its model is
// what an Anthropic link without a model resolves to, so it is Haiku (never defaultAnthropicModel,
// which is Sonnet).
func lfAnthropicConfig() AnthropicConfig {
	return AnthropicConfig{Model: DefaultHaikuAPIID(), MaxTokens: lfMaxTokens}
}

// lfNewChat is the chat primitive of planTurn: the tenant's "chat" chain (tenant row, then global
// row, then code default) through llm.RunChat with a 15-second limit per call. JSON mode, temperature
// and the token cap come from planTurn's request.
//
// The Anthropic client only matters for a chain link without a model, i.e. the code default when
// nothing is configured (Encore without a Gemini key): that link is Haiku, never Sonnet, so an
// unconfigured tenant cannot silently pay the Sonnet price on every message.
func lfNewChat(tenantID string) chatFunc {
	reg := LLMRegistry(secrets.AnthropicAPIKey, lfAnthropicConfig())
	return func(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
		cctx, cancel := context.WithTimeout(ctx, lfCallTimeout)
		defer cancel()
		resp, res, err := llm.RunChat(cctx, reg, tenantID, llm.FnChat, req)
		rlog.Info("llm_first chat",
			"tenantId", tenantID, "provider", resp.Usage.Provider, "model", resp.Usage.Model,
			"attempts", res.Attempts, "inTok", resp.Usage.InputTokens, "outTok", resp.Usage.OutputTokens,
			"thoughtTok", resp.Usage.ThoughtsTokens, "finish", resp.Usage.FinishReason, "ok", err == nil)
		return resp, err
	}
}

// ── live side effects ────────────────────────────────────────────────────────

// lfLiveFX applies the effects of an LLM-first turn to Redis and the tenant database, with the
// same functions the legacy flow uses: setOrderState / clearOrderState for the checkout,
// persistDraftOrder for the order, cancelPersistedOrder for a cancellation, pauseAI for a handoff.
type lfLiveFX struct {
	s *AutoReplyService
	t lfTurn
}

func (f lfLiveFX) setState(ctx context.Context, st orderState) {
	f.s.setOrderState(ctx, f.t.payload.TenantID, f.t.convo.ID, st)
}

func (f lfLiveFX) clearState(ctx context.Context) {
	f.s.clearOrderState(ctx, f.t.payload.TenantID, f.t.convo.ID)
}

func (f lfLiveFX) persistOrder(ctx context.Context, st orderState) (string, error) {
	return persistDraftOrder(ctx, f.t.ts, f.t.payload.TenantSchema, f.t.convo.ID, f.t.convo.ContactID, st)
}

func (f lfLiveFX) persistEarlyDraft(ctx context.Context, st orderState) (string, bool, error) {
	id, needPick, _, err := persistDraftOrderEarly(ctx, f.t.ts, f.t.payload.TenantSchema, f.t.convo.ID, f.t.convo.ContactID, st)
	return id, needPick, err
}

func (f lfLiveFX) cancelOrder(ctx context.Context, orderID string) error {
	return cancelPersistedOrder(ctx, f.t.ts, f.t.payload.TenantSchema, orderID, f.t.scope())
}

func (f lfLiveFX) pause(ctx context.Context, reason string) error {
	return pauseAI(ctx, f.t.ts, f.t.convo.ID, reason)
}

func (f lfLiveFX) notify(ctx context.Context, kind, title, body string) {
	f.s.notifyOwner(ctx, f.t.payload.TenantID, f.t.convo.ID, kind, title, body)
}

// ── per-conversation Redis state ─────────────────────────────────────────────

func lfRemindKey(tenantID, convoID string) string {
	return fmt.Sprintf("ai:openorder:remind:%s:%s", tenantID, convoID)
}

func lfConfusedKey(tenantID, convoID string) string {
	return fmt.Sprintf("ai:llmfirst:confused:%s:%s", tenantID, convoID)
}

func (s *AutoReplyService) loadRemindState(ctx context.Context, tenantID, convoID string) (salesbrain.ReminderState, error) {
	raw, err := s.rdb.Get(ctx, lfRemindKey(tenantID, convoID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return salesbrain.ReminderState{}, nil
	}
	if err != nil {
		return salesbrain.ReminderState{}, err
	}
	return lfDecodeRemind(raw), nil
}

func (s *AutoReplyService) saveRemindState(ctx context.Context, tenantID, convoID string, rs salesbrain.ReminderState) {
	if err := s.rdb.Set(ctx, lfRemindKey(tenantID, convoID), lfEncodeRemind(rs), lfRemindTTL).Err(); err != nil {
		rlog.Warn("llm_first: reminder state not saved", "convoId", convoID, "err", err)
	}
}

func (s *AutoReplyService) loadConfused(ctx context.Context, tenantID, convoID string) (int, error) {
	n, err := s.rdb.Get(ctx, lfConfusedKey(tenantID, convoID)).Int()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	return n, err
}

// saveConfused stores the counter after a turn; a reset only touches Redis when there was
// something to reset.
func (s *AutoReplyService) saveConfused(ctx context.Context, tenantID, convoID string, prev, next int, reset bool) {
	key := lfConfusedKey(tenantID, convoID)
	var err error
	switch {
	case reset && prev == 0:
		return
	case reset:
		err = s.rdb.Del(ctx, key).Err()
	default:
		err = s.rdb.Set(ctx, key, next, lfConfusedTTL).Err()
	}
	if err != nil {
		rlog.Warn("llm_first: confusion counter not saved", "convoId", convoID, "err", err)
	}
}

// ── shadow mode ──────────────────────────────────────────────────────────────

// startLLMFirstShadow runs the pipeline for its log line only, beside the legacy flow. The turn is
// copied first: the legacy flow keeps mutating the contact, conversation and profile it shares, and
// the goroutine must not race with it. The shadow run has no channel and no lfFX: it cannot send,
// persist a language, write Redis state, pause or notify. A panic is recovered here so a bug in the
// experiment can never take down the reply of the legacy flow.
func (s *AutoReplyService) startLLMFirstShadow(ctx context.Context, t lfTurn) {
	contact, convo, profile := *t.contact, *t.convo, *t.profile
	t.contact, t.convo, t.profile, t.channel = &contact, &convo, &profile, nil
	go func() {
		defer func() {
			if r := recover(); r != nil {
				rlog.Error("llm_first shadow: panic recovered", "convoId", t.convo.ID, "panic", fmt.Sprint(r))
			}
		}()
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), lfShadowTimeout)
		defer cancel()
		s.shadowLLMFirst(sctx, t)
	}()
}

// shadowLLMFirst loads, decides the language and plans the turn, then logs one line. It records the
// call as an AI activity (purpose llm_first_shadow, so the cost shows in the dashboards) and as an
// ai_token_llm_first event (pipeline llm_first_shadow) to measure the cost per message, but never as
// an ai_token event: shadow tokens are our experiment and must not eat the tenant's quota.
func (s *AutoReplyService) shadowLLMFirst(ctx context.Context, t lfTurn) {
	ld, err := llmFirstLoad(s, ctx, t)
	if err != nil {
		rlog.Warn("llm_first shadow: load failed", "convoId", t.convo.ID, "err", err)
		return
	}
	dec := salesbrain.DecideLanguage(contactPreferredLanguage(t.contact), salesbrain.LangID, t.userText, lastOutboundWasLanguageAsk(ld.lastBody))
	if dec.AskChoice { // the live pipeline answers this one with the language question, no LLM call
		rlog.Info("llm_first shadow", "convoId", t.convo.ID, "inboundId", t.payload.InboundMessageID,
			"intent", "language_ask", "lang", string(dec.Lang), "fallback", false)
		return
	}
	locked := contactPreferredLanguage(t.contact) != "" || dec.Persist != ""
	in := s.lfPlanInputFor(ctx, t, ld, dec, locked)
	plan := planTurn(ctx, in, llmFirstChatFor(t.payload.TenantID))

	if tokens := lfTokens(plan); tokens > 0 {
		lfRecordLLMFirstTokens(ctx, t, "shadow", lfShadowPurpose, plan)
		meta := AiReplyMeta{
			Reason: reasonAIGenerated, Path: PathLLMFirst, Model: plan.Usage.Model, LLMUsed: true,
			Pipeline: lfPipeline, Intent: string(plan.Decision.Intent), Lang: string(plan.Lang), Attempts: plan.Attempts,
		}
		recordActivity(ctx, meta, lfShadowPurpose, int(plan.Usage.InputTokens), int(plan.Usage.OutputTokens+plan.Usage.ThoughtsTokens))
	}
	rlog.Info("llm_first shadow",
		"convoId", t.convo.ID, "inboundId", t.payload.InboundMessageID,
		"intent", string(plan.Decision.Intent), "lang", string(plan.Lang), "langPersist", string(dec.Persist),
		"fallback", plan.Fallback, "why", plan.FallbackWhy, "attempts", plan.Attempts,
		"violations", lfViolationCodes(plan.Violations),
		"cartChanged", plan.CartChanged, "handoff", string(lfHandoffLevel(plan.Decision)),
		"replyPreview", previewText(plan.Reply, 200),
		"model", plan.Usage.Model, "inTok", plan.Usage.InputTokens, "outTok", plan.Usage.OutputTokens,
		"thoughtTok", plan.Usage.ThoughtsTokens)
}
