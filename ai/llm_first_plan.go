package ai

import (
	"context"
	"strings"
	"unicode/utf8"

	"encore.app/wabantu/internal/salesbrain"
	"encore.app/wabantu/shared/llm"
)

// chatFunc is the single-call chat primitive planTurn needs. The I/O shell wraps llm.RunChat
// (with a per-call timeout); tests inject a fake.
type chatFunc func(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error)

// planInput is everything planTurn needs, already loaded by the caller.
type planInput struct {
	Lang       salesbrain.LanguageDecision
	LangLocked bool
	Profile    salesbrain.ProfileFact
	Catalog    []salesbrain.CatalogFact
	KB         []salesbrain.KBFact
	State      salesbrain.TurnState // Cart/PendingField/OpenOrders/RemindAllowed already filled
	// PendingAfterVariant is the pending field once the cart's variants are settled (what follows
	// the "which size?" question). Equal to State.PendingField when that is not "variant".
	PendingAfterVariant string
	History             []salesbrain.HistoryTurn
	Message             string
	BuyerTexts          []string // current + up to 3 previous buyer messages
}

// turnPlan is the outcome of one planTurn call. Nothing in it has been applied or sent.
type turnPlan struct {
	Decision     salesbrain.Decision
	Lang         salesbrain.Lang
	Cart         []salesbrain.OrderLine // cart after item ops (== State.Cart when unchanged)
	CartChanged  bool
	UnknownItems []int
	// Issues are the cart ops that were refused or left incomplete (missing variant, sold out,
	// stock limit). When there are any, Reply is built from templates, not from the model's text.
	Issues         []salesbrain.OpIssue
	ReplacedReason string // why the model's reply was replaced ("" when it was kept)
	Reply          string // final text to send (model reply + code additions)
	Fallback       bool   // true → do not send; legacy flow handles the turn
	FallbackWhy    string
	Attempts       int
	Usage          llm.Usage              // summed across attempts (Input/Output/Thoughts)
	RawOutput      string                 // last raw model output, redacted
	Violations     []salesbrain.Violation // of the last attempt
}

// Fallback reasons reported in turnPlan.FallbackWhy.
const (
	lfWhyLLMError      = "llm_error"
	lfWhyInvalidOutput = "invalid_output"
)

// Reason recorded in message metadata when cart issues replaced the model's reply.
const lfReplacedCartIssue = "cart_issue"

const (
	lfMaxAttempts      = 2    // one call plus one corrective retry
	lfMaxTokens        = 1024 // per call
	lfTemperature      = 0.3
	lfRawOutputRunes   = 1000 // cap of the redacted raw output kept for message metadata
	lfMaxComposedRunes = 1400 // model reply (≤ salesbrain.MaxReplyRunes) + summary + resume line
	lfFieldAfterItems  = "recipient"

	lfLanguageSwitchNote = "CATATAN SISTEM: pembeli baru saja memilih bahasa ini; buka balasan dengan satu kalimat konfirmasi dalam bahasa ini."
	lfBrokenJSONNote     = "\n\nCATATAN SISTEM: balasan sebelumnya bukan JSON valid. Kirim HANYA satu objek JSON sesuai format KELUARAN."
)

// planTurn is the pure decision core of the LLM-first pipeline (spec §3.1). It builds the
// grounded prompt, makes at most two JSON chat calls (the second one carries a correction note),
// applies the proposed cart operations, validates the reply against the facts the buyer may
// legitimately be quoted, and composes the final text. It receives already-loaded inputs and an
// injected chat function, writes nothing and sends nothing, so it is unit-testable with a fake
// LLM. A turnPlan with Fallback=true must not be sent: the legacy flow handles the turn.
func planTurn(ctx context.Context, in planInput, chat chatFunc) turnPlan {
	lang := in.Lang.Lang
	plan := turnPlan{Lang: lang, Cart: in.State.Cart}

	// The language lock and the cart total are decided by code, never taken from the caller's
	// copy of the state block: derive them here so the prompt can not contradict the decision.
	state := in.State
	state.Lang = lang
	state.LangLocked = in.LangLocked
	state.CartSubtotal = salesbrain.CartSubtotal(state.Cart)
	if state.PendingField != "" && state.ResumeLine == "" {
		state.ResumeLine = salesbrain.ResumeLineFor(state.PendingField, lang)
	}

	system, user := salesbrain.BuildPrompt(salesbrain.PromptInput{
		Profile: in.Profile,
		Catalog: in.Catalog,
		KB:      in.KB,
		State:   state,
		History: in.History,
		Message: in.Message,
	})
	if in.Lang.Switched {
		user = lfLanguageSwitchNote + "\n\n" + user
	}

	var (
		note     string
		accepted salesbrain.Decision
		cart     []salesbrain.OrderLine
		changed  bool
		unknown  []int
		issues   []salesbrain.OpIssue
		ok       bool
	)
	for attempt := 1; attempt <= lfMaxAttempts && !ok; attempt++ {
		plan.Attempts = attempt
		plan.Decision = salesbrain.Decision{}
		plan.Violations = nil

		temperature := lfTemperature
		resp, err := chat(ctx, llm.ChatRequest{
			System:      system,
			Messages:    []llm.Message{{Role: "user", Text: user + note}},
			MaxTokens:   lfMaxTokens,
			JSON:        true,
			Temperature: &temperature,
		})
		lfAddUsage(&plan.Usage, resp.Usage)
		if err != nil {
			return lfFallbackPlan(plan, lfWhyLLMError)
		}
		plan.RawOutput = salesbrain.RedactForLog(resp.Text, lfRawOutputRunes)

		d, err := salesbrain.ParseDecision(resp.Text)
		if err != nil {
			note = lfBrokenJSONNote
			continue
		}
		plan.Decision = d

		nextCart, nextChanged, nextUnknown, nextIssues := in.State.Cart, false, []int(nil), []salesbrain.OpIssue(nil)
		if d.Intent == salesbrain.IntentOrder || d.Intent == salesbrain.IntentAmend {
			res := salesbrain.ResolveCartOps(salesbrain.ResolveInput{
				Cart: in.State.Cart, Ops: d.Items, Catalog: in.Catalog, BuyerTexts: in.BuyerTexts,
			})
			nextCart, nextChanged, nextUnknown, nextIssues = res.Cart, res.Changed, res.Unknown, res.Issues
		}
		facts := salesbrain.BuildFacts(salesbrain.FactsInput{
			Catalog:      in.Catalog,
			KB:           in.KB,
			Cart:         nextCart,
			CartSubtotal: salesbrain.CartSubtotal(nextCart),
			OpenOrders:   in.State.OpenOrders,
			BuyerTexts:   in.BuyerTexts,
		})
		if vs := salesbrain.Validate(d, facts, lang); len(vs) > 0 {
			plan.Violations = vs
			note = "\n\n" + salesbrain.ViolationNote(vs)
			continue
		}
		accepted, cart, changed, unknown, issues, ok = d, nextCart, nextChanged, nextUnknown, nextIssues, true
	}
	if !ok {
		return lfFallbackPlan(plan, lfWhyInvalidOutput)
	}

	plan.Cart, plan.CartChanged, plan.UnknownItems, plan.Issues = cart, changed, unknown, issues
	pending := lfPendingAfterOps(in, cart)
	if len(issues) > 0 {
		// The model's text may promise what the code refused (a sold-out size "added to the cart"):
		// the buyer hears the code's own sentences instead.
		plan.Reply = lfComposeIssueReply(issues, cart, changed, pending, lang)
		plan.ReplacedReason = lfReplacedCartIssue
		return plan
	}
	plan.Reply = lfComposeReply(accepted.Reply, cart, changed, pending, lang)
	return plan
}

// lfPendingAfterOps is the checkout field the reply must lead back to once the cart ops are
// applied: "variant" while a line lacks its size or colour, and once the last one is filled the
// field that was waiting behind the variant question.
func lfPendingAfterOps(in planInput, cart []salesbrain.OrderLine) string {
	switch {
	case lfCartMissingVariant(cart):
		return "variant"
	case in.State.PendingField == "variant":
		return in.PendingAfterVariant
	}
	return in.State.PendingField
}

// lfComposeIssueReply renders the refused ops (templates, locked language), the cart summary when
// the cart changed, and the resume line unless an issue already ends on a question.
func lfComposeIssueReply(issues []salesbrain.OpIssue, cart []salesbrain.OrderLine, changed bool, pendingField string, lang salesbrain.Lang) string {
	reply := salesbrain.RenderIssues(issues, lang)
	next := pendingField
	if len(cart) == 0 {
		next = ""
	} else if changed && next == "" {
		next = lfFieldAfterItems
	}
	resume := ""
	if next != "" && !strings.Contains(reply, "?") {
		resume = salesbrain.ResumeLineFor(next, lang)
	}
	compose := func(withSummary bool) string {
		out := reply
		if withSummary {
			if summary := salesbrain.RenderCartSummary(cart, lang); summary != "" {
				out += "\n\n" + summary
			}
		}
		if resume != "" {
			out += "\n\n" + resume
		}
		return out
	}
	out := compose(changed)
	if changed && utf8.RuneCountInString(out) > lfMaxComposedRunes {
		out = compose(false)
	}
	return out
}

// lfFallbackPlan marks the plan as "do not send": the legacy flow handles the turn.
func lfFallbackPlan(plan turnPlan, why string) turnPlan {
	plan.Fallback, plan.FallbackWhy, plan.Reply = true, why, ""
	return plan
}

// lfAddUsage sums the token counters of one call into the running total. Provider, model and
// finish reason come from the latest call that reported them (a failed call reports none).
func lfAddUsage(total *llm.Usage, u llm.Usage) {
	total.InputTokens += u.InputTokens
	total.OutputTokens += u.OutputTokens
	total.ThoughtsTokens += u.ThoughtsTokens
	total.CacheReadInputTokens += u.CacheReadInputTokens
	total.CacheCreationInputTokens += u.CacheCreationInputTokens
	total.CachedTokens += u.CachedTokens
	if u.Provider != "" {
		total.Provider = u.Provider
	}
	if u.Model != "" {
		total.Model = u.Model
	}
	if u.FinishReason != "" {
		total.FinishReason = u.FinishReason
	}
}

// lfComposeReply appends the code-owned parts to the model reply: the cart summary after a cart
// change and the line that resumes the pending checkout field, unless the model already ended
// on a question of its own (one call to action per message). When the composed text is too long
// the summary is dropped rather than the answer.
func lfComposeReply(modelReply string, cart []salesbrain.OrderLine, changed bool, pendingField string, lang salesbrain.Lang) string {
	next := pendingField
	switch {
	case len(cart) == 0:
		next = "" // an empty cart has no pending field
	case changed && next == "":
		next = lfFieldAfterItems
	}
	resume := ""
	if next != "" && !strings.Contains(modelReply, "?") {
		resume = salesbrain.ResumeLineFor(next, lang)
	}

	compose := func(withSummary bool) string {
		reply := modelReply
		if withSummary {
			if summary := salesbrain.RenderCartSummary(cart, lang); summary != "" {
				reply += "\n\n" + summary
			}
		}
		if resume != "" {
			reply += "\n\n" + resume
		}
		return reply
	}
	reply := compose(changed)
	if changed && utf8.RuneCountInString(reply) > lfMaxComposedRunes {
		reply = compose(false)
	}
	return reply
}
