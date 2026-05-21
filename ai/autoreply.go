package ai

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"encore.dev/rlog"
	"encore.dev/storage/sqldb"

	"encore.app/wabantu/shared/inboxrealtime"
	"encore.app/wabantu/usage"
	"encore.app/wabantu/whatsapp"
	"github.com/redis/go-redis/v9"
)

// ─── Constants ───────────────────────────────────────────────────────────────

const (
	reasonAIGenerated      = "ai_generated"
	reasonProfileIncomplete = "profile_incomplete"
	reasonNonQuestion      = "non_question"
	reasonOutOfScope       = "out_of_scope"
	llmConfidenceThreshold = 0.65
	maxReplyLen            = 1200
	faqCacheTTL            = 24 * time.Hour
	orderStateTTL          = 2 * time.Hour
	counterTTL             = 6 * time.Hour
)

// ─── DB & Redis ──────────────────────────────────────────────────────────────

var aiDB = sqldb.Named("tenant")

func withTenantDB(ctx context.Context, schema string) (*sql.DB, error) {
	stdlib := aiDB.Stdlib()
	_, err := stdlib.ExecContext(ctx, fmt.Sprintf(`SET search_path TO %q`, schema))
	if err != nil {
		return nil, fmt.Errorf("set search_path: %w", err)
	}
	return stdlib, nil
}

// ─── Payload & internal types ────────────────────────────────────────────────

type AiReplyJobPayload struct {
	TenantID         string `json:"tenantId"`
	TenantSchema     string `json:"tenantSchema"`
	ConversationID   string `json:"conversationId"`
	InboundMessageID string `json:"inboundMessageId"`
}

type dbConversation struct {
	ID            string
	ContactID     string
	ChannelID     string
	AIHandled     bool
	AIPausedAt    *time.Time
	HandoffReason *string
	LastMessageAt *time.Time
	LastPreview   *string
	Status        string
}

type dbMessage struct {
	ID        string
	Direction string
	Author    string
	Type      string
	Body      string
	CreatedAt time.Time
}

type dbBusinessProfile struct {
	BusinessName     string
	Description      *string
	Address          *string
	OpeningHours     *string
	ProductsServices *string
	BasePricing      *string
	DeliveryArea     *string
	GreetingTemplate *string
	Tone             *string
	AIEnabled        bool
	CatalogURL       *string
}

type dbContact struct {
	ID          string
	PhoneNumber string
	DisplayName *string
}

type dbChannel struct {
	ID               string
	Provider         string
	Status           string
	AccessToken      *string
	MetaPhoneNumberID *string
	MetaWabaID       *string
	DisplayName      string
	PhoneNumber      string
}

type dbKBEntry struct {
	ID       string
	Question string
	Answer   string
	Category *string
	IsActive bool
}

type classifyResult struct {
	Label      string
	Confidence float64
}

type orderState struct {
	Step    string `json:"step"`
	Product string `json:"product,omitempty"`
	Variant string `json:"variant,omitempty"`
	Qty     int    `json:"qty,omitempty"`
}

// ─── AutoReplyService ────────────────────────────────────────────────────────

type AutoReplyService struct {
	rdb       *redis.Client
	anthropic *AnthropicClient
}

func NewAutoReplyService(rdb *redis.Client, anthropicClient *AnthropicClient) *AutoReplyService {
	return &AutoReplyService{
		rdb:       rdb,
		anthropic: anthropicClient,
	}
}

// ProcessAutoReply is the main AI auto-reply pipeline.
func (s *AutoReplyService) ProcessAutoReply(ctx context.Context, payload AiReplyJobPayload) (bool, error) {
	rlog.Info("AI job start",
		"tenantId", payload.TenantID,
		"convoId", payload.ConversationID,
		"inboundId", payload.InboundMessageID,
	)

	db, err := withTenantDB(ctx, payload.TenantSchema)
	if err != nil {
		return false, err
	}

	convo, err := loadConversation(ctx, db, payload.ConversationID)
	if err != nil {
		return false, err
	}
	if convo == nil {
		rlog.Warn("AI job: missing conversation")
		return false, nil
	}
	ctx = WithActivityContext(ctx, ActivityContext{
		TenantSchema:     payload.TenantSchema,
		TenantID:         payload.TenantID,
		ConversationID:   convo.ID,
		InboundMessageID: payload.InboundMessageID,
	})

	inbound, err := loadMessage(ctx, db, payload.InboundMessageID)
	if err != nil {
		return false, err
	}
	if inbound == nil {
		rlog.Warn("AI job: missing inbound message")
		return false, nil
	}

	if !convo.AIHandled {
		rlog.Warn("AI job: convo.aiHandled=false", "convoId", convo.ID)
		return false, fmt.Errorf("AI_HANDOFF_PAUSED")
	}
	if inbound.Direction != "in" || inbound.Author != "contact" {
		rlog.Warn("AI job: inbound not a contact inbound")
		return false, nil
	}
	if inbound.Type != "text" {
		rlog.Warn("AI job: inbound type not supported", "type", inbound.Type)
		return false, nil
	}

	profile, err := loadBusinessProfile(ctx, db)
	if err != nil {
		return false, err
	}
	if profile == nil || !profile.AIEnabled {
		rlog.Warn("AI job: aiEnabled=false")
		return false, nil
	}

	contact, err := loadContact(ctx, db, convo.ContactID)
	if err != nil {
		return false, err
	}
	channel, err := loadChannel(ctx, db, convo.ChannelID)
	if err != nil {
		return false, err
	}
	if contact == nil || channel == nil {
		rlog.Warn("AI job: missing contact or channel")
		return false, nil
	}
	if channel.Provider != "meta_cloud" || channel.Status != "connected" {
		rlog.Warn("AI job: unsupported/invalid channel",
			"provider", channel.Provider,
			"status", channel.Status,
		)
		return false, nil
	}
	if channel.AccessToken == nil || channel.MetaPhoneNumberID == nil {
		rlog.Warn("AI job: channel missing accessToken or metaPhoneNumberId")
		return false, nil
	}

	bp := toBusinessProfile(profile)

	if !isBusinessProfileComplete(profile) {
		out := metaNoLLM(reasonProfileIncomplete, PathProfileIncomplete)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, db, payload.TenantID, convo, channel, contact,
			nonAiDefaultReply(profile), "system", out)
		return err == nil, err
	}

	userText := SanitizeForPrompt(inbound.Body)
	rlog.Info("AI job: inbound text", "lenUserText", len(userText))

	if IsGreetingLike(userText) {
		greet := GreetingReply(userText, strOrEmpty(profile.Tone), strOrEmpty(profile.GreetingTemplate))
		out := metaNoLLM(reasonNonQuestion, PathGreeting)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, db, payload.TenantID, convo, channel, contact, greet, "ai", out)
		return err == nil, err
	}

	if IsGreetingFeedback(userText) {
		greet := GreetingFeedbackReply(userText, strOrEmpty(profile.Tone))
		out := metaNoLLM(reasonNonQuestion, PathGreeting)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, db, payload.TenantID, convo, channel, contact, greet, "ai", out)
		return err == nil, err
	}

	if IsPromptInjectionLikely(userText) {
		rlog.Warn("Potential prompt injection",
			"tenantId", payload.TenantID,
			"convoId", convo.ID,
		)
		spike, _ := s.bumpCounter(ctx, payload.TenantID, convo.ID, "offscope")
		text := "Maaf kak, untuk pertanyaan teknis sistem tidak bisa saya proses. Untuk info produk, harga, stok, atau pengiriman, aku bantu ya 🙏"
		if spike >= 3 {
			text = nonAiDefaultReply(profile)
		}
		out := metaNoLLM(reasonOutOfScope, PathInjectionGuard)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, db, payload.TenantID, convo, channel, contact, text, "system", out)
		return err == nil, err
	}

	history, err := loadHistory(ctx, db, convo.ID, 12)
	if err != nil {
		return false, err
	}
	kbEntries, err := loadKBEntries(ctx, db, 20)
	if err != nil {
		return false, err
	}

	// Active order flow — only when message is really continuing checkout (not greeting/harga/batal).
	if orderSt, _ := s.getOrderState(ctx, payload.TenantID, convo.ID); orderSt != nil {
		tone := strOrEmpty(profile.Tone)
		if IsOrderFlowCancelled(userText) {
			s.clearOrderState(ctx, payload.TenantID, convo.ID)
			out := metaNoLLM(reasonNonQuestion, PathOrderFlow)
			out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
			err = s.sendAiMessage(ctx, db, payload.TenantID, convo, channel, contact,
				orderFlowCancelReply(tone), "system", out)
			return err == nil, err
		}
		if ShouldBreakOrderFlow(userText, orderSt.Step) {
			s.clearOrderState(ctx, payload.TenantID, convo.ID)
			rlog.Info("AI job: order flow cleared for new intent", "prevStep", orderSt.Step)
			if IsGreetingLike(userText) {
				greet := GreetingReply(userText, tone, strOrEmpty(profile.GreetingTemplate))
				out := metaNoLLM(reasonNonQuestion, PathGreeting)
				out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
				err = s.sendAiMessage(ctx, db, payload.TenantID, convo, channel, contact, greet, "ai", out)
				return err == nil, err
			}
			// Fall through → classifier / LLM for harga, tanya produk, dll.
		} else {
			sent, oErr := s.handleOrderFlow(ctx, db, payload.TenantSchema, payload.TenantID, convo, channel, contact,
				userText, profile, kbEntries, payload.InboundMessageID)
			return sent, oErr
		}
	}

	var scopeParts []string
	scopeParts = append(scopeParts, profile.BusinessName)
	scopeParts = append(scopeParts, strOrEmpty(profile.Description))
	scopeParts = append(scopeParts, strOrEmpty(profile.ProductsServices))
	scopeParts = append(scopeParts, strOrEmpty(profile.BasePricing))
	scopeParts = append(scopeParts, strOrEmpty(profile.DeliveryArea))
	for _, kb := range kbEntries {
		scopeParts = append(scopeParts, kb.Question+" "+kb.Answer)
	}
	scopeKeywords := ExtractScopeKeywords(strings.Join(scopeParts, " "))

	fallbackKW := []string{
		"harga", "stok", "produk", "order", "pengiriman", "ukuran", "size",
		"mau", "tanya", "beli", "ada", "celana", "jeans", "baju", "apparel",
	}
	inScope := IsWithinBusinessScope(userText, scopeKeywords, fallbackKW)
	if !inScope && IsActiveCheckoutFromHistory(history, userText) {
		inScope = true
	}
	classifier := classifyMessage(userText, inScope, profile)
	if classifier.Label == "in_scope_non_question" &&
		(HasPurchaseIntent(userText) || IsOrderFollowUpFromHistory(history, userText)) {
		classifier = classifyResult{Label: "order_intent", Confidence: 0.85}
	}
	if ac, ok := ActivityContextFrom(ctx); ok {
		ac.Classifier = classifier.Label
		ctx = WithActivityContext(ctx, ac)
	}
	rlog.Info("AI job: scope check",
		"inScope", inScope,
		"classifier", classifier.Label,
		"userPreview", previewText(userText, 80),
	)
	rlog.Info("AI job: classifier",
		"label", classifier.Label,
		"confidence", classifier.Confidence,
	)

	// ── Handle: sensitive escalation ─────────────────────────────────────
	if classifier.Label == "sensitive_escalate" {
		out := metaNoLLM(reasonOutOfScope, PathEscalate)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, db, payload.TenantID, convo, channel, contact,
			"Maaf kak, untuk topik ini tim CS kami akan langsung mengambil alih dan segera menghubungi kakak 🙏",
			"system", out)
		if err != nil {
			return false, err
		}
		return true, pauseAI(ctx, db, convo.ID, "Sensitive/escalate detected")
	}

	// ── Handle: out of scope ─────────────────────────────────────────────
	if classifier.Label == "out_of_scope" {
		c, _ := s.bumpCounter(ctx, payload.TenantID, convo.ID, "offscope")
		rlog.Warn("AI job: out-of-business-scope", "count", c)
		text := outOfScopeReply(profile)
		if c >= 3 {
			text = nonAiDefaultReply(profile)
		}
		out := metaNoLLM(reasonOutOfScope, PathOutOfScope)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, db, payload.TenantID, convo, channel, contact, text, "system", out)
		return err == nil, err
	}

	// ── Handle: in-scope non-question ────────────────────────────────────
	if classifier.Label == "in_scope_non_question" {
		c, _ := s.bumpCounter(ctx, payload.TenantID, convo.ID, "nonscope")
		rlog.Warn("AI job: in-scope non-question", "count", c)
		text := scopeDirectionReply(profile)
		if c >= 3 {
			text = nonAiDefaultReply(profile)
		}
		out := metaNoLLM(reasonNonQuestion, PathNonQuestion)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, db, payload.TenantID, convo, channel, contact, text, "system", out)
		return err == nil, err
	}

	// ── Handle: low-confidence question ──────────────────────────────────
	if classifier.Label == "in_scope_question" && classifier.Confidence < llmConfidenceThreshold {
		out := metaNoLLM(reasonNonQuestion, PathLowConfidence)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, db, payload.TenantID, convo, channel, contact,
			scopeDirectionReply(profile), "system", out)
		return err == nil, err
	}

	// ── Handle: order intent state machine ───────────────────────────────
	if classifier.Label == "order_intent" || (IsOrderContinuationMessage(userText) && hasOrderIntentText(userText)) {
		sent, oErr := s.handleOrderFlow(ctx, db, payload.TenantSchema, payload.TenantID, convo, channel, contact,
			userText, profile, kbEntries, payload.InboundMessageID)
		return sent, oErr
	}

	// ── In-scope question → LLM path ────────────────────────────────────
	s.resetScopeCounters(ctx, payload.TenantID, convo.ID)

	cached, _ := s.getCachedAnswer(ctx, payload.TenantID, userText)
	if cached != "" {
		out := metaNoLLM(reasonAIGenerated, PathFAQCache)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, db, payload.TenantID, convo, channel, contact, cached, "ai", out)
		return err == nil, err
	}

	kbHybrid := retrieveHybridKB(userText, kbEntries)
	kbTopScore := topKBMatchScore(userText, kbEntries)
	rlog.Info("AI job: hybrid KB retrieval",
		"selected", len(kbHybrid),
		"total", len(kbEntries),
		"topScore", kbTopScore,
	)

	// FAQ bypass — no LLM call when KB match is strong (cost optimization).
	if direct, ok := tryFAQDirectAnswer(userText, kbEntries); ok {
		finalReply := applyOutputPolicy(direct)
		s.setCachedAnswer(ctx, payload.TenantID, userText, finalReply)
		out := metaNoLLM(reasonAIGenerated, PathFAQDirect)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, db, payload.TenantID, convo, channel, contact, finalReply, "ai", out)
		return err == nil, err
	}

	planCode, _ := loadSubscriptionPlanCode(ctx, db)
	complexity := ClassifyComplexity(userText, classifier.Label, kbTopScore)
	route := ResolveRouting(planCode, complexity)
	if ac, ok := ActivityContextFrom(ctx); ok {
		ac.RouteReason = route.Reason
		ctx = WithActivityContext(ctx, ac)
	}
	rlog.Info("AI job: hybrid model routing",
		"plan", planCode,
		"complexity", complexity,
		"model", route.Model,
		"tier", route.Tier,
		"routeReason", route.Reason,
	)

	if ok, reason := usage.CheckAICostLimit(ctx, payload.TenantSchema, payload.TenantID); !ok {
		rlog.Warn("AI job: tenant cost limit reached", "reason", reason)
		out := metaNoLLM(reasonOutOfScope, PathCostLimit)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, db, payload.TenantID, convo, channel, contact,
			"Maaf kak, kuota AI bulan ini sudah mencapai batas. Tim kami akan segera menghubungi kakak ya 🙏",
			"system", out)
		return err == nil, err
	}

	kbForPrompt := make([]KBEntry, len(kbHybrid))
	for i, e := range kbHybrid {
		cat := ""
		if e.Category != nil {
			cat = *e.Category
		}
		kbForPrompt[i] = KBEntry{Question: e.Question, Answer: e.Answer, Category: cat}
	}
	histForPrompt := make([]HistoryMessage, len(history))
	for i, m := range history {
		histForPrompt[i] = HistoryMessage{Author: m.Author, Body: m.Body, Type: m.Type}
	}

	sys := BuildSystemPrompt(bp)
	business := BuildBusinessContext(bp)
	kbCtx := BuildKnowledgeContext(kbForPrompt)
	summary, _ := GetLatestSummary(ctx, payload.TenantSchema, convo.ID)
	histCtx := BuildConversationContextWithSummary(summary, histForPrompt)
	rlog.Info("AI job: context sizes",
		"sys", len(sys),
		"business", len(business),
		"kb", len(kbCtx),
		"hist", len(histCtx),
	)

	reply, compUsage, err := s.anthropic.GenerateReplyWithModel(ctx, route.Model, sys, business, kbCtx, histCtx, userText)
	if err != nil {
		rlog.Error("AI job: anthropic.GenerateReply failed",
			"err", err,
			"model", route.Model,
			"tenantId", payload.TenantID,
			"convoId", convo.ID,
		)
		return false, err
	}

	_, _, newSession := usage.TrackAIExchange(ctx, payload.TenantID, convo.ID)
	tokens := compUsage.InputTokens + compUsage.OutputTokens
	usage.RecordAITokens(ctx, payload.TenantID, convo.ID, tokens)
	if newSession {
		_ = usage.RecordEvent(ctx, payload.TenantSchema, "ai_conversation", 1, nil)
	}
	if tokens > 0 {
		_ = usage.RecordEvent(ctx, payload.TenantSchema, "ai_token", tokens, nil)
	}

	finalReply := applyOutputPolicy(reply)
	s.setCachedAnswer(ctx, payload.TenantID, userText, finalReply)
	out := metaFromRoute(reasonAIGenerated, PathLLM, route)
	out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, compUsage.InputTokens, compUsage.OutputTokens)
	err = s.sendAiMessage(ctx, db, payload.TenantID, convo, channel, contact, finalReply, "ai", out)
	return err == nil, err
}

// FallbackAutoReply sends a generic fallback and pauses AI on the conversation.
func (s *AutoReplyService) FallbackAutoReply(ctx context.Context, payload AiReplyJobPayload) error {
	db, err := withTenantDB(ctx, payload.TenantSchema)
	if err != nil {
		return err
	}

	convo, err := loadConversation(ctx, db, payload.ConversationID)
	if err != nil || convo == nil {
		return err
	}
	contact, err := loadContact(ctx, db, convo.ContactID)
	if err != nil {
		return err
	}
	channel, err := loadChannel(ctx, db, convo.ChannelID)
	if err != nil {
		return err
	}
	if contact == nil || channel == nil || channel.AccessToken == nil || channel.MetaPhoneNumberID == nil {
		return nil
	}

	out := metaNoLLM(reasonNonQuestion, PathAutoFallback)
	out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
	err = s.sendAiMessage(ctx, db, payload.TenantID, convo, channel, contact,
		"Maaf kak, saat ini sistem kami sedang sibuk. Tim kami akan bantu balas secepatnya ya 🙏",
		"system", out)
	if err != nil {
		return err
	}
	return pauseAI(ctx, db, convo.ID, "Auto fallback setelah retry AI gagal")
}

// ─── Order flow state machine ────────────────────────────────────────────────

var (
	sizeRe   = regexp.MustCompile(`(?i)\b(xs|s|m|l|xl|xxl|xxxl|3xl|4xl|5xl|\d{2})\b`)
	qtyRe    = regexp.MustCompile(`(?i)\b(\d{1,3})\s?(pcs|biji|buah|item)?\b`)
	addrRe   = regexp.MustCompile(`(?i)(jalan|jl\.|rt|rw|kel\.|kec\.|kota|kab\.|kode pos)`)
)

func (s *AutoReplyService) handleOrderFlow(
	ctx context.Context,
	db *sql.DB,
	tenantSchema, tenantID string,
	convo *dbConversation,
	channel *dbChannel,
	contact *dbContact,
	userText string,
	profile *dbBusinessProfile,
	kb []dbKBEntry,
	inboundID string,
) (bool, error) {
	scopeKW := businessScopeKeywords(profile)
	if IsOffBusinessProductRequest(userText, scopeKW) {
		s.clearOrderState(ctx, tenantID, convo.ID)
		out := metaNoLLM(reasonOutOfScope, PathOutOfScope)
		out.LogAndRecord(ctx, convo.ID, inboundID, 0, 0)
		err := s.sendAiMessage(ctx, db, tenantID, convo, channel, contact,
			outOfScopeReply(profile), "system", out)
		return err == nil, err
	}

	tmpl := orderTemplatesFromKB(kb, strOrEmpty(profile.Tone) == "formal")
	send := func(text string) (bool, error) {
		out := metaNoLLM(reasonNonQuestion, PathOrderFlow)
		out.LogAndRecord(ctx, convo.ID, inboundID, 0, 0)
		err := s.sendAiMessage(ctx, db, tenantID, convo, channel, contact, text, "system", out)
		return err == nil, err
	}

	state, _ := s.getOrderState(ctx, tenantID, convo.ID)
	hints := parseOrderHints(userText)

	if state == nil {
		if hints.HasSize && strings.TrimSpace(hints.Product) != "" {
			st := orderState{Product: hints.Product, Variant: hints.Variant}
			if hints.HasQty {
				st.Qty = hints.Qty
				st.Step = "ask_address"
				s.setOrderState(ctx, tenantID, convo.ID, st)
				return send(tmpl.AskAddress)
			}
			st.Step = "ask_qty"
			s.setOrderState(ctx, tenantID, convo.ID, st)
			return send(tmpl.AskQty)
		}
		s.setOrderState(ctx, tenantID, convo.ID, orderState{Step: "ask_product"})
		return send(tmpl.AskProduct)
	}

	switch state.Step {
	case "ask_product":
		if IsOffBusinessProductRequest(userText, scopeKW) {
			s.clearOrderState(ctx, tenantID, convo.ID)
			out := metaNoLLM(reasonOutOfScope, PathOutOfScope)
			out.LogAndRecord(ctx, convo.ID, inboundID, 0, 0)
			err := s.sendAiMessage(ctx, db, tenantID, convo, channel, contact,
				outOfScopeReply(profile), "system", out)
			return err == nil, err
		}
		product := hints.Product
		if product == "" {
			product = userText
		}
		if len(product) > 120 {
			product = product[:120]
		}
		variant := hints.Variant
		if variant == "" {
			if m := sizeRe.FindString(userText); m != "" {
				variant = m
			}
		}
		if variant != "" || hints.HasSize {
			st := orderState{Product: product, Variant: variant, Step: "ask_qty"}
			if hints.HasQty {
				st.Qty = hints.Qty
				st.Step = "ask_address"
				s.setOrderState(ctx, tenantID, convo.ID, st)
				return send(tmpl.AskAddress)
			}
			s.setOrderState(ctx, tenantID, convo.ID, st)
			return send(tmpl.AskQty)
		}
		s.setOrderState(ctx, tenantID, convo.ID, orderState{Step: "ask_variant", Product: product})
		return send(tmpl.AskVariant)

	case "ask_variant":
		variant := userText
		if m := sizeRe.FindString(userText); m != "" {
			variant = m
		} else if len(variant) > 60 {
			variant = variant[:60]
		}
		st := orderState{Step: "ask_qty", Product: state.Product, Variant: variant}
		if hints.HasQty {
			st.Qty = hints.Qty
			st.Step = "ask_address"
			s.setOrderState(ctx, tenantID, convo.ID, st)
			return send(tmpl.AskAddress)
		}
		s.setOrderState(ctx, tenantID, convo.ID, st)
		return send(tmpl.AskQty)

	case "ask_qty":
		qty := 0
		if hints.HasQty {
			qty = hints.Qty
		} else if m := qtyRe.FindStringSubmatch(userText); len(m) > 1 {
			fmt.Sscanf(m[1], "%d", &qty)
		}
		if qty < 1 {
			return send(tmpl.ClarifyQty)
		}
		s.setOrderState(ctx, tenantID, convo.ID, orderState{
			Step: "ask_address", Product: state.Product, Variant: state.Variant, Qty: qty,
		})
		return send(tmpl.AskAddress)

	case "ask_address":
		if addrRe.MatchString(userText) {
			st := orderState{
				Product: state.Product, Variant: state.Variant, Qty: state.Qty, Step: "done",
			}
			if _, err := persistDraftOrder(ctx, db, tenantSchema, convo.ID, convo.ContactID, st, userText); err != nil {
				rlog.Warn("AI order: persist draft failed", "err", err, "convoId", convo.ID)
			}
			s.clearOrderState(ctx, tenantID, convo.ID)
			return send(tmpl.Complete)
		}
	}

	return send(tmpl.RetryStep)
}

// ─── Message classifier ──────────────────────────────────────────────────────

var sensitiveKeywords = []string{
	"penipuan", "fraud", "komplain keras", "lapor polisi",
	"ancam", "refund gagal", "tagihan salah",
}

func classifyMessage(userText string, inScope bool, profile *dbBusinessProfile) classifyResult {
	text := strings.ToLower(userText)

	for _, kw := range sensitiveKeywords {
		if strings.Contains(text, kw) {
			return classifyResult{Label: "sensitive_escalate", Confidence: 0.98}
		}
	}
	if !inScope {
		return classifyResult{Label: "out_of_scope", Confidence: 0.9}
	}
	if HasPurchaseIntent(userText) {
		if IsOffBusinessProductRequest(userText, businessScopeKeywords(profile)) {
			return classifyResult{Label: "out_of_scope", Confidence: 0.92}
		}
		// "mau pesen jeans bisa?" = tanya dulu, bukan form order.
		capabilityAsk := IsQuestionLike(userText) &&
			!orderQtyLineRe.MatchString(text) && !orderSizeLineRe.MatchString(text)
		if !capabilityAsk {
			return classifyResult{Label: "order_intent", Confidence: 0.88}
		}
	}
	if IsQuestionLike(userText) {
		return classifyResult{Label: "in_scope_question", Confidence: 0.8}
	}
	return classifyResult{Label: "in_scope_non_question", Confidence: 0.8}
}

// ─── Hybrid KB retrieval ─────────────────────────────────────────────────────

func tokenize(text string) []string {
	lower := strings.ToLower(text)
	cleaned := nonAlphaNum.ReplaceAllString(lower, " ")
	words := strings.Fields(cleaned)
	var out []string
	for _, w := range words {
		if len(w) >= 2 {
			out = append(out, w)
		}
	}
	return out
}

func overlapScore(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	setA := make(map[string]struct{}, len(a))
	for _, x := range a {
		setA[x] = struct{}{}
	}
	setB := make(map[string]struct{}, len(b))
	for _, x := range b {
		setB[x] = struct{}{}
	}
	intersection := 0
	for x := range setA {
		if _, ok := setB[x]; ok {
			intersection++
		}
	}
	denom := len(setA) + len(setB)
	if denom == 0 {
		return 0
	}
	return float64(2*intersection) / float64(denom)
}

type scoredKB struct {
	entry dbKBEntry
	score float64
}

func retrieveHybridKB(query string, kb []dbKBEntry) []dbKBEntry {
	qTokens := tokenize(query)
	qScope := ExtractScopeKeywords(query)

	scored := make([]scoredKB, 0, len(kb))
	for _, entry := range kb {
		text := entry.Question + " " + entry.Answer
		eTokens := tokenize(text)
		lexical := overlapScore(qTokens, eTokens)
		semantic := overlapScore(qScope, ExtractScopeKeywords(text))
		rerank := overlapScore(tokenize(entry.Question), qTokens)
		score := lexical*0.5 + semantic*0.3 + rerank*0.2
		if score > 0 {
			scored = append(scored, scoredKB{entry: entry, score: score})
		}
	}

	// Sort descending by score (simple insertion sort for small N).
	for i := 1; i < len(scored); i++ {
		for j := i; j > 0 && scored[j].score > scored[j-1].score; j-- {
			scored[j], scored[j-1] = scored[j-1], scored[j]
		}
	}

	limit := 8
	if len(scored) < limit {
		limit = len(scored)
	}
	result := make([]dbKBEntry, limit)
	for i := 0; i < limit; i++ {
		result[i] = scored[i].entry
	}
	return result
}

// ─── Output policy ───────────────────────────────────────────────────────────

var (
	codeBlockRe   = regexp.MustCompile("(?s)```.*?```")
	mdLinkRe      = regexp.MustCompile(`\[(.*?)\]\((.*?)\)`)
	blockedOutput = regexp.MustCompile(`(?i)(system prompt|api key|database|server root|drop table)`)
)

func applyOutputPolicy(text string) string {
	cleaned := codeBlockRe.ReplaceAllString(text, "")
	cleaned = mdLinkRe.ReplaceAllString(cleaned, "$1")
	cleaned = strings.TrimSpace(cleaned)
	if len(cleaned) > maxReplyLen {
		cleaned = cleaned[:maxReplyLen]
	}
	if blockedOutput.MatchString(cleaned) {
		return "Maaf kak, untuk keamanan kami hanya bisa bantu pertanyaan seputar produk, harga, stok, dan order ya 🙏"
	}
	return cleaned
}

// ─── Reply helpers ───────────────────────────────────────────────────────────

func nonAiDefaultReply(profile *dbBusinessProfile) string {
	scope := strings.TrimSpace(strOrEmpty(profile.ProductsServices))
	if scope == "" {
		return "Maaf kak, tim CS kami akan segera menghubungi kakak untuk bantu lebih lanjut ya 🙏"
	}
	return fmt.Sprintf("Maaf kak, tim CS kami akan segera menghubungi kakak. Saat ini kami fokus bantu seputar: %s.", scope)
}

func scopeDirectionReply(profile *dbBusinessProfile) string {
	scope := strings.TrimSpace(strOrEmpty(profile.ProductsServices))
	if scope == "" {
		return "Boleh kak, biar tepat, bisa tanya seputar produk, harga, stok, atau pengiriman dari bisnis kami ya."
	}
	return fmt.Sprintf("Boleh kak, biar tepat kami bantu pertanyaan seputar: %s. Bisa tanya harga/stok/order ya.", scope)
}

func outOfScopeReply(profile *dbBusinessProfile) string {
	scope := strings.TrimSpace(strOrEmpty(profile.ProductsServices))
	if scope == "" {
		return "Maaf kak, itu di luar topik bisnis kami ya. Tim CS kami akan bantu follow-up jika diperlukan."
	}
	return fmt.Sprintf("Maaf kak, itu di luar topik bisnis kami ya. Kami fokus pada: %s.", scope)
}

func isBusinessProfileComplete(p *dbBusinessProfile) bool {
	filled := func(s *string) bool {
		return s != nil && strings.TrimSpace(*s) != ""
	}
	return len(strings.TrimSpace(p.BusinessName)) >= 2 &&
		filled(p.Description) &&
		filled(p.Address) &&
		filled(p.OpeningHours) &&
		filled(p.ProductsServices) &&
		filled(p.BasePricing) &&
		filled(p.DeliveryArea)
}

func toBusinessProfile(p *dbBusinessProfile) BusinessProfile {
	return BusinessProfile{
		BusinessName:     p.BusinessName,
		Description:      strOrEmpty(p.Description),
		Address:          strOrEmpty(p.Address),
		OpeningHours:     strOrEmpty(p.OpeningHours),
		ProductsServices: strOrEmpty(p.ProductsServices),
		BasePricing:      strOrEmpty(p.BasePricing),
		DeliveryArea:     strOrEmpty(p.DeliveryArea),
		GreetingTemplate: strOrEmpty(p.GreetingTemplate),
		Tone:             strOrEmpty(p.Tone),
		CatalogURL:       strOrEmpty(p.CatalogURL),
	}
}

func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ─── Redis helpers ───────────────────────────────────────────────────────────

func normalizeQuestion(text string) string {
	norm := strings.Join(tokenize(text), " ")
	if len(norm) > 240 {
		norm = norm[:240]
	}
	return norm
}

func faqCacheKey(tenantID, normalized string) string {
	h := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("ai:faqcache:%s:%s", tenantID, hex.EncodeToString(h[:]))
}

func (s *AutoReplyService) getCachedAnswer(ctx context.Context, tenantID, userText string) (string, error) {
	norm := normalizeQuestion(userText)
	if norm == "" {
		return "", nil
	}
	val, err := s.rdb.Get(ctx, faqCacheKey(tenantID, norm)).Result()
	if err == redis.Nil {
		return "", nil
	}
	return val, err
}

func (s *AutoReplyService) setCachedAnswer(ctx context.Context, tenantID, userText, answer string) {
	norm := normalizeQuestion(userText)
	if norm == "" {
		return
	}
	s.rdb.Set(ctx, faqCacheKey(tenantID, norm), answer, faqCacheTTL)
}

func (s *AutoReplyService) getOrderState(ctx context.Context, tenantID, convoID string) (*orderState, error) {
	key := fmt.Sprintf("ai:order:%s:%s", tenantID, convoID)
	raw, err := s.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st orderState
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return nil, nil
	}
	return &st, nil
}

func (s *AutoReplyService) setOrderState(ctx context.Context, tenantID, convoID string, st orderState) {
	key := fmt.Sprintf("ai:order:%s:%s", tenantID, convoID)
	data, _ := json.Marshal(st)
	s.rdb.Set(ctx, key, data, orderStateTTL)
}

func (s *AutoReplyService) clearOrderState(ctx context.Context, tenantID, convoID string) {
	key := fmt.Sprintf("ai:order:%s:%s", tenantID, convoID)
	s.rdb.Del(ctx, key)
}

func counterKey(kind, tenantID, convoID string) string {
	return fmt.Sprintf("ai:counter:%s:%s:%s", kind, tenantID, convoID)
}

func (s *AutoReplyService) bumpCounter(ctx context.Context, tenantID, convoID, kind string) (int, error) {
	key := counterKey(kind, tenantID, convoID)
	n, err := s.rdb.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	s.rdb.Expire(ctx, key, counterTTL)
	return int(n), nil
}

func (s *AutoReplyService) resetScopeCounters(ctx context.Context, tenantID, convoID string) {
	s.rdb.Del(ctx,
		counterKey("offscope", tenantID, convoID),
		counterKey("nonscope", tenantID, convoID),
	)
}

// ─── DB loaders ──────────────────────────────────────────────────────────────

func loadSubscriptionPlanCode(ctx context.Context, db *sql.DB) (string, error) {
	var planCode string
	var isTrial bool
	err := db.QueryRowContext(ctx, `
		SELECT plan_code, COALESCE(is_trial, false) FROM subscription
		WHERE status = 'active'
		ORDER BY created_at DESC LIMIT 1`,
	).Scan(&planCode, &isTrial)
	if err == sql.ErrNoRows {
		return "trial", nil
	}
	if err != nil {
		return "starter", err
	}
	if isTrial {
		return "trial", nil
	}
	if planCode == "basic" {
		return "business", nil
	}
	return planCode, nil
}

func loadConversation(ctx context.Context, db *sql.DB, id string) (*dbConversation, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, contact_id, channel_id, ai_handled, ai_paused_at,
		       handoff_reason, last_message_at, last_message_preview, status
		FROM conversation WHERE id = $1`, id)
	c := &dbConversation{}
	err := row.Scan(&c.ID, &c.ContactID, &c.ChannelID, &c.AIHandled,
		&c.AIPausedAt, &c.HandoffReason, &c.LastMessageAt, &c.LastPreview, &c.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

func loadMessage(ctx context.Context, db *sql.DB, id string) (*dbMessage, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, direction, author, type, COALESCE(body,''), created_at
		FROM message WHERE id = $1`, id)
	m := &dbMessage{}
	err := row.Scan(&m.ID, &m.Direction, &m.Author, &m.Type, &m.Body, &m.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return m, err
}

func loadBusinessProfile(ctx context.Context, db *sql.DB) (*dbBusinessProfile, error) {
	row := db.QueryRowContext(ctx, `
		SELECT business_name, description, address, opening_hours,
		       products_services, base_pricing, delivery_area,
		       greeting_template, tone, ai_enabled,
		       COALESCE(catalog_website_url, '')
		FROM business_profile ORDER BY created_at ASC LIMIT 1`)
	p := &dbBusinessProfile{}
	var catalogURL string
	err := row.Scan(&p.BusinessName, &p.Description, &p.Address, &p.OpeningHours,
		&p.ProductsServices, &p.BasePricing, &p.DeliveryArea,
		&p.GreetingTemplate, &p.Tone, &p.AIEnabled, &catalogURL)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if catalogURL != "" {
		p.CatalogURL = &catalogURL
	}
	return p, err
}

func loadContact(ctx context.Context, db *sql.DB, id string) (*dbContact, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, phone_number, display_name
		FROM contact WHERE id = $1`, id)
	c := &dbContact{}
	err := row.Scan(&c.ID, &c.PhoneNumber, &c.DisplayName)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

func loadChannel(ctx context.Context, db *sql.DB, id string) (*dbChannel, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, provider, status, access_token, meta_phone_number_id,
		       meta_waba_id, display_name, phone_number
		FROM whatsapp_channel WHERE id = $1`, id)
	ch := &dbChannel{}
	err := row.Scan(&ch.ID, &ch.Provider, &ch.Status, &ch.AccessToken,
		&ch.MetaPhoneNumberID, &ch.MetaWabaID, &ch.DisplayName, &ch.PhoneNumber)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return ch, err
}

func loadHistory(ctx context.Context, db *sql.DB, convoID string, limit int) ([]dbMessage, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, direction, author, type, COALESCE(body,''), created_at
		FROM message WHERE conversation_id = $1
		ORDER BY created_at DESC LIMIT $2`, convoID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var msgs []dbMessage
	for rows.Next() {
		var m dbMessage
		if err := rows.Scan(&m.ID, &m.Direction, &m.Author, &m.Type, &m.Body, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	// Reverse to chronological order.
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, rows.Err()
}

func loadKBEntries(ctx context.Context, db *sql.DB, limit int) ([]dbKBEntry, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, question, answer, category, is_active
		FROM knowledge_base_entry
		WHERE is_active = true
		ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []dbKBEntry
	for rows.Next() {
		var e dbKBEntry
		if err := rows.Scan(&e.ID, &e.Question, &e.Answer, &e.Category, &e.IsActive); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ─── DB writers ──────────────────────────────────────────────────────────────

func (s *AutoReplyService) sendAiMessage(
	ctx context.Context,
	db *sql.DB,
	tenantID string,
	convo *dbConversation,
	channel *dbChannel,
	contact *dbContact,
	text, author string,
	meta AiReplyMeta,
) error {
	rlog.Info("AI job: sending WhatsApp text",
		"len", len(text),
		"author", author,
		"path", meta.Path,
		"reason", meta.Reason,
		"llmUsed", meta.LLMUsed,
		"model", meta.Model,
		"tier", meta.Tier,
	)

	if channel == nil || contact == nil {
		return fmt.Errorf("missing channel or contact")
	}
	if channel.Provider != "meta_cloud" {
		return fmt.Errorf("unsupported channel provider %q", channel.Provider)
	}
	if channel.Status != "connected" {
		return fmt.Errorf("whatsapp channel not connected")
	}
	if channel.AccessToken == nil || strings.TrimSpace(*channel.AccessToken) == "" {
		return fmt.Errorf("whatsapp channel missing access token")
	}
	if channel.MetaPhoneNumberID == nil || strings.TrimSpace(*channel.MetaPhoneNumberID) == "" {
		return fmt.Errorf("whatsapp channel missing meta_phone_number_id")
	}
	contactPhone := whatsapp.NormalizeRecipient(contact.PhoneNumber)
	if contactPhone == "" {
		return fmt.Errorf("invalid contact phone")
	}

	extID, err := whatsapp.SendText(
		ctx,
		*channel.AccessToken,
		*channel.MetaPhoneNumberID,
		contactPhone,
		text,
	)
	if err != nil {
		rlog.Error("AI job: meta SendText failed",
			"err", err,
			"convoId", convo.ID,
			"channelId", channel.ID,
		)
		return fmt.Errorf("send whatsapp: %w", err)
	}

	preview := text
	if len(preview) > 280 {
		preview = preview[:280]
	}

	metadataJSON, _ := json.Marshal(meta)
	var msgCreatedAt time.Time
	err = db.QueryRowContext(ctx, `
		INSERT INTO message (conversation_id, external_id, direction, author, type, body, metadata, status)
		VALUES ($1, $2, 'out', $3, 'text', $4, $5::jsonb, 'sent')
		RETURNING created_at`,
		convo.ID, extID, author, text, string(metadataJSON),
	).Scan(&msgCreatedAt)
	if err != nil {
		rlog.Error("AI job: failed inserting outbound message", "err", err)
		return fmt.Errorf("insert message: %w", err)
	}

	_, err = db.ExecContext(ctx, `
		UPDATE conversation
		SET last_message_at = $1, last_message_preview = $2, status = 'open'
		WHERE id = $3`,
		msgCreatedAt, preview, convo.ID,
	)
	if err != nil {
		rlog.Error("AI job: failed updating conversation", "err", err)
		return fmt.Errorf("update conversation: %w", err)
	}

	if tenantID != "" && s.rdb != nil {
		inboxrealtime.Publish(ctx, s.rdb, tenantID)
	}
	return nil
}

func pauseAI(ctx context.Context, db *sql.DB, convoID, reason string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE conversation
		SET ai_handled = false, ai_paused_at = NOW(), handoff_reason = $1
		WHERE id = $2`,
		reason, convoID,
	)
	return err
}
