package ai

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"encore.dev/rlog"
	"encore.dev/storage/sqldb"

	appflag "encore.app/wabantu/flag"
	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/shared/pii"
	"encore.app/wabantu/shared/retrieval"
	"encore.app/wabantu/shared/strutil"
	"encore.app/wabantu/shared/whatsappchannel"
	"encore.app/wabantu/usage"
	"encore.app/wabantu/whatsapp"
	"github.com/redis/go-redis/v9"
)

// ─── Constants ───────────────────────────────────────────────────────────────

// ErrAIHandoffPaused means staff has taken over the conversation; inbound AI must not reply or retry.
var ErrAIHandoffPaused = errors.New("AI_HANDOFF_PAUSED")

const (
	reasonAIGenerated       = "ai_generated"
	reasonProfileIncomplete = "profile_incomplete"
	reasonNonQuestion       = "non_question"
	reasonOutOfScope        = "out_of_scope"
	llmConfidenceThreshold  = 0.65
	maxReplyLen             = 1200
	faqCacheTTL             = 24 * time.Hour
	orderStateTTL           = 2 * time.Hour
	counterTTL              = 6 * time.Hour
)

// ─── DB & Redis ──────────────────────────────────────────────────────────────

var aiDB = sqldb.Named("tenant")

// tenantQuerier is implemented by poolQuerier, *sql.Tx, and tenantScope.
type tenantQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) appdb.Scannable
	ExecContext(context.Context, string, ...any) (sql.Result, error)
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

type dbContact struct {
	ID          string
	PhoneNumber string
	DisplayName *string
}

type dbChannel struct {
	ID                string
	Provider          string
	Status            string
	AccessToken       *string
	MetaPhoneNumberID *string
	MetaWabaID        *string
	DisplayName       string
	PhoneNumber       string
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

	ts, err := openTenantScope(ctx, payload.TenantSchema)
	if err != nil {
		return false, err
	}

	convo, err := loadConversation(ctx, ts, payload.ConversationID)
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

	inbound, err := loadMessage(ctx, ts, payload.InboundMessageID)
	if err != nil {
		return false, err
	}
	if inbound == nil {
		rlog.Warn("AI job: missing inbound message")
		return false, nil
	}

	if !convo.AIHandled {
		rlog.Warn("AI job: convo.aiHandled=false", "convoId", convo.ID)
		return false, ErrAIHandoffPaused
	}
	if inbound.Direction != "in" || inbound.Author != "contact" {
		rlog.Warn("AI job: inbound not a contact inbound")
		return false, nil
	}
	userText, processable := inboundTextForAutoReply(inbound.Type, inbound.Body)
	if !processable {
		if isMediaTypeWithOptionalCaption(inbound.Type) {
			rlog.Info("AI job: media inbound without caption, skip", "type", inbound.Type)
		} else {
			rlog.Warn("AI job: inbound type not supported", "type", inbound.Type)
		}
		return false, nil
	}
	if IsPaymentProofInbound(inbound.Type, userText) {
		rlog.Info("AI job: payment proof inbound, skip autoreply", "type", inbound.Type)
		return false, nil
	}

	profile, err := loadBusinessProfile(ctx, ts)
	if err != nil {
		return false, err
	}
	if profile == nil || !profile.AIEnabled {
		rlog.Warn("AI job: aiEnabled=false")
		return false, nil
	}

	contact, err := loadContact(ctx, ts, convo.ContactID)
	if err != nil {
		return false, err
	}
	channel, err := loadChannel(ctx, ts, convo.ChannelID)
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
		err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact,
			nonAiDefaultReply(profile), "system", payload.InboundMessageID, out)
		return err == nil, err
	}

	rlog.Info("AI job: inbound text", "lenUserText", len(userText), "sourceType", inbound.Type)

	if IsThirdPartyBuyerLookup(userText) {
		return s.handleThirdPartyBuyerLookupDenied(ctx, ts, payload, convo, channel, contact)
	}

	// Status inquiry sebelum greeting — "halo min punya pesanan aktif?" bukan sapaan murni.
	// Recap keranjang chat dulu (trace Redis), baru status DB jika checkout kosong.
	if earlySt, _ := s.getOrderState(ctx, payload.TenantID, convo.ID); PreferCheckoutRecapOverDBStatus(userText, CheckoutStateHasRecap(earlySt)) {
		formal := strOrEmpty(profile.Tone) == "formal"
		reply := CartRecapReply(*earlySt, formal)
		out := metaNoLLM(reasonNonQuestion, PathOrderFlow)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, reply, "system", payload.InboundMessageID, out)
		return err == nil, err
	}
	if (IsOrderStatusInquiry(userText) || IsSelfBuyerOrderLookup(userText) || IsOrderRefStatusLookup(userText)) && !wantsOrderContextFromHistory(userText) {
		return s.handleCustomerOrderStatus(ctx, ts, payload.TenantSchema, payload, convo, channel, contact, userText, nil)
	}

	if IsGreetingLike(userText) {
		// Sapaan baru = sesi baru; jangan biarkan draft order Redis mengganggu pertanyaan berikutnya.
		s.clearOrderState(ctx, payload.TenantID, convo.ID)
		greet := GreetingReply(userText, strOrEmpty(profile.Tone), strOrEmpty(profile.GreetingTemplate))
		out := metaNoLLM(reasonNonQuestion, PathGreeting)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, greet, "ai", payload.InboundMessageID, out)
		return err == nil, err
	}

	if IsGreetingFeedback(userText) {
		greet := GreetingFeedbackReply(userText, strOrEmpty(profile.Tone))
		out := metaNoLLM(reasonNonQuestion, PathGreeting)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, greet, "ai", payload.InboundMessageID, out)
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
		err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, text, "system", payload.InboundMessageID, out)
		return err == nil, err
	}

	history, err := loadHistory(ctx, ts, convo.ID, 12)
	if err != nil {
		return false, err
	}
	kbEntries, err := loadKBEntries(ctx, ts, 20)
	if err != nil {
		return false, err
	}

	catalog, catLoadErr := loadActiveCatalog(ctx, ts, defaultCatalogLoadLimit)
	if catLoadErr != nil {
		rlog.Warn("AI job: catalog load failed", "err", catLoadErr)
	}
	enrichCatalogStock(ctx, ts, catalog)

	if IsPaymentQuestion(userText) {
		if ans, ok := tryPaymentFAQAnswer(userText, kbEntries); ok {
			finalReply := applyOutputPolicy(ans)
			out := metaNoLLM(reasonAIGenerated, PathPaymentFAQ)
			out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
			err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, finalReply, "ai", payload.InboundMessageID, out)
			return err == nil, err
		}
	}

	if IsOrderCancelRequest(userText) {
		orderSt, _ := s.getOrderState(ctx, payload.TenantID, convo.ID)
		if orderSt != nil && strings.TrimSpace(orderSt.PersistedOrderID) != "" {
			scope := orderAccessScope{ConversationID: convo.ID, ContactID: contact.ID}
			if cerr := cancelPersistedOrder(ctx, ts, payload.TenantSchema, orderSt.PersistedOrderID, scope); cerr != nil && !errors.Is(cerr, sql.ErrNoRows) {
				rlog.Warn("AI order cancel: persisted draft", "err", cerr, "orderId", orderSt.PersistedOrderID)
			}
		}
		s.clearOrderState(ctx, payload.TenantID, convo.ID)
		if orderSt != nil {
			tone := strOrEmpty(profile.Tone)
			out := metaNoLLM(reasonNonQuestion, PathOrderCancel)
			out.OrderAction = "cancel_draft"
			out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
			err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact,
				orderFlowCancelReply(tone), "system", payload.InboundMessageID, out)
			return err == nil, err
		}
		// Tanpa draft aktif: "ga jadi beli + ada nomor pesanan?" → jawab status, jangan cancel order lama.
		if IsOrderStatusInquiry(userText) || IsSelfBuyerOrderLookup(userText) {
			return s.handleCustomerOrderStatus(ctx, ts, payload.TenantSchema, payload, convo, channel, contact, userText, history)
		}
		if ShouldCancelPersistedOrder(userText) {
			return s.handleCustomerOrderCancel(ctx, ts, payload.TenantSchema, payload, convo, channel, contact, profile, userText)
		}
		out := metaNoLLM(reasonNonQuestion, PathOrderCancel)
		out.OrderAction = "cancel"
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact,
			orderNoneToCancelReply(), "system", payload.InboundMessageID, out)
		return err == nil, err
	}
	if IsThirdPartyBuyerLookup(userText) {
		return s.handleThirdPartyBuyerLookupDenied(ctx, ts, payload, convo, channel, contact)
	}
	if IsCartRecapOrComplaint(userText, toBFCatalogSlice(catalog)) || IsActiveCheckoutRecapQuestion(userText) {
		if orderSt, _ := s.getOrderState(ctx, payload.TenantID, convo.ID); orderSt != nil {
			formal := strOrEmpty(profile.Tone) == "formal"
			reply := CartRecapReply(*orderSt, formal)
			out := metaNoLLM(reasonNonQuestion, PathOrderFlow)
			out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
			err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, reply, "system", payload.InboundMessageID, out)
			return err == nil, err
		}
	}
	if IsOrderStatusInquiry(userText) || IsSelfBuyerOrderLookup(userText) || IsOrderRefStatusLookup(userText) {
		return s.handleCustomerOrderStatus(ctx, ts, payload.TenantSchema, payload, convo, channel, contact, userText, history)
	}

	clearedOrderForCorrection := false

	// Active order flow — only when message is really continuing checkout (not greeting/harga/batal).
	if orderSt, _ := s.getOrderState(ctx, payload.TenantID, convo.ID); orderSt != nil {
		tone := strOrEmpty(profile.Tone)
		formal := tone == "formal"
		if IsAddMoreItemsPolicyQuestion(userText) {
			reply := applyOutputPolicy(AddMoreItemsPolicyReply(formal, orderSt))
			out := metaNoLLM(reasonNonQuestion, PathOrderFlow)
			out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
			err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, reply, "system", payload.InboundMessageID, out)
			return err == nil, err
		}
		if ShouldBreakOrderFlow(userText, orderSt.Step, catalog) {
			if ShouldKeepCartOnExplicitNewOrder(orderSt, userText) {
				sent, oErr := s.handleOrderFlow(ctx, ts, payload.TenantSchema, payload.TenantID, convo, channel, contact,
					userText, profile, kbEntries, history, payload.InboundMessageID)
				return sent, oErr
			}
			clearedOrderForCorrection = IsUserSalesCorrection(userText)
			s.clearOrderState(ctx, payload.TenantID, convo.ID)
			rlog.Info("AI job: order flow cleared for new intent", "prevStep", orderSt.Step, "correction", clearedOrderForCorrection)
			if IsGreetingLike(userText) {
				greet := GreetingReply(userText, tone, strOrEmpty(profile.GreetingTemplate))
				out := metaNoLLM(reasonNonQuestion, PathGreeting)
				out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
				err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, greet, "ai", payload.InboundMessageID, out)
				return err == nil, err
			}
			// Fall through → classifier / LLM for harga, tanya produk, dll.
		} else {
			sent, oErr := s.handleOrderFlow(ctx, ts, payload.TenantSchema, payload.TenantID, convo, channel, contact,
				userText, profile, kbEntries, history, payload.InboundMessageID)
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
	if !inScope && (IsActiveCheckoutFromHistory(history, userText) || IsAcknowledgmentLike(userText)) {
		inScope = true
	}

	orderActiveNow, _ := s.getOrderState(ctx, payload.TenantID, convo.ID)
	intent := ResolveSalesIntent(userText, history, orderActiveNow != nil, inScope, profile, catalog)
	classifier := salesIntentToClassifier(intent)
	// Fallback ke classifier lama untuk edge case intent confidence rendah.
	if intent.Confidence < 0.72 {
		legacy := classifyMessage(userText, inScope, profile)
		if legacy.Label == "sensitive_escalate" || legacy.Label == "order_intent" || legacy.Label == "out_of_scope" {
			classifier = legacy
		}
	}
	if IsAcknowledgmentLike(userText) {
		classifier = classifyResult{Label: "in_scope_question", Confidence: 0.85}
	} else if intent.State == SalesStateCartReady {
		classifier = classifyResult{Label: "order_intent", Confidence: intent.Confidence}
	} else if intent.State == SalesStateSensitive {
		classifier = classifyResult{Label: "sensitive_escalate", Confidence: intent.Confidence}
	} else if intent.State == SalesStateOutOfScope {
		classifier = classifyResult{Label: "out_of_scope", Confidence: intent.Confidence}
	}
	if ac, ok := ActivityContextFrom(ctx); ok {
		ac.Classifier = classifier.Label + ":" + intent.State
		ctx = WithActivityContext(ctx, ac)
	}

	rlog.Info("AI job: scope check",
		"inScope", inScope,
		"classifier", classifier.Label,
		"salesState", intent.State,
		"salesTopic", intent.Topic,
		"userPreview", previewText(userText, 80),
	)
	rlog.Info("AI job: classifier",
		"label", classifier.Label,
		"confidence", classifier.Confidence,
		"salesState", intent.State,
	)

	// ── Order flow — prioritas sebelum katalog statis ─────────────────────
	if inScope && (classifier.Label == "order_intent" || IsStructuredOrderList(userText)) {
		sent, oErr := s.handleOrderFlow(ctx, ts, payload.TenantSchema, payload.TenantID, convo, channel, contact,
			userText, profile, kbEntries, history, payload.InboundMessageID)
		return sent, oErr
	}

	// ── Kebijakan pesan atas nama penerima lain — sebelum katalog (hindari hijack Abon dll.) ──
	if inScope && IsRecipientPolicyQuestion(userText) {
		formal := strOrEmpty(profile.Tone) == "formal"
		finalReply := applyOutputPolicy(replyRecipientPolicyQuestion(userText, kbEntries, formal))
		out := metaNoLLM(reasonAIGenerated, PathRecipientPolicy)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, finalReply, "ai", payload.InboundMessageID, out)
		return err == nil, err
	}

	if inScope && IsAddMoreItemsPolicyQuestion(userText) {
		formal := strOrEmpty(profile.Tone) == "formal"
		orderSt, _ := s.getOrderState(ctx, payload.TenantID, convo.ID)
		reply := applyOutputPolicy(AddMoreItemsPolicyReply(formal, orderSt))
		out := metaNoLLM(reasonAIGenerated, PathConsulting)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, reply, "ai", payload.InboundMessageID, out)
		return err == nil, err
	}

	if inScope && IsOrderAmendMessage(userText) {
		return s.handleOrderAmend(ctx, ts, payload, convo, channel, contact, userText, profile, history)
	}

	// ── Katalog WABantu (business_catalog_item) — prioritas sebelum FAQ/LLM ──
	if inScope {
		if catReply, ok := s.replyFromBusinessCatalogHybrid(ctx, payload.TenantID, payload.TenantSchema, userText, ts, profile, catalog, history); ok {
			if clearedOrderForCorrection {
				catReply = prependSalesCorrection(strOrEmpty(profile.Tone) == "formal", catReply)
			}
			finalReply := applyOutputPolicy(catReply)
			out := metaNoLLM(reasonAIGenerated, PathCatalogDB)
			out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
			err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, finalReply, "ai", payload.InboundMessageID, out)
			return err == nil, err
		}
		if clearedOrderForCorrection {
			formal := strOrEmpty(profile.Tone) == "formal"
			out := metaNoLLM(reasonAIGenerated, PathConsulting)
			out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
			err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact,
				salesCorrectionReply(formal), "ai", payload.InboundMessageID, out)
			return err == nil, err
		}
	}

	// ── Shipping FAQ — ongkir/estimasi kirim sebelum FAQ umum/LLM ──
	if inScope {
		formal := strOrEmpty(profile.Tone) == "formal"
		if shipReply, ok := tryShippingFAQReply(userText, profile, kbEntries, formal); ok {
			finalReply := applyOutputPolicy(shipReply)
			out := metaNoLLM(reasonAIGenerated, PathShippingFAQ)
			out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
			err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, finalReply, "ai", payload.InboundMessageID, out)
			return err == nil, err
		}
	}

	// ── Handle: sensitive escalation ─────────────────────────────────────
	if classifier.Label == "sensitive_escalate" {
		out := metaNoLLM(reasonOutOfScope, PathEscalate)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact,
			"Maaf kak, untuk topik ini tim CS kami akan langsung mengambil alih dan segera menghubungi kakak 🙏",
			"system", payload.InboundMessageID, out)
		if err != nil {
			return false, err
		}
		return true, pauseAI(ctx, ts, convo.ID, "Sensitive/escalate detected")
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
		err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, text, "system", payload.InboundMessageID, out)
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
		err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, text, "system", payload.InboundMessageID, out)
		return err == nil, err
	}

	// ── Handle: low-confidence question ──────────────────────────────────
	if classifier.Label == "in_scope_question" && classifier.Confidence < llmConfidenceThreshold {
		out := metaNoLLM(reasonNonQuestion, PathLowConfidence)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact,
			scopeDirectionReply(profile), "system", payload.InboundMessageID, out)
		return err == nil, err
	}

	// ── Handle: order intent state machine ───────────────────────────────
	if classifier.Label == "order_intent" || (IsOrderContinuationMessage(userText) && hasOrderIntentText(userText)) {
		sent, oErr := s.handleOrderFlow(ctx, ts, payload.TenantSchema, payload.TenantID, convo, channel, contact,
			userText, profile, kbEntries, history, payload.InboundMessageID)
		return sent, oErr
	}
	if inScope && (IsOrderRevisionMessage(userText) ||
		(mentionsOrderQty(userText) && IsActiveCheckoutFromHistory(history, userText))) {
		sent, oErr := s.handleOrderFlow(ctx, ts, payload.TenantSchema, payload.TenantID, convo, channel, contact,
			userText, profile, kbEntries, history, payload.InboundMessageID)
		return sent, oErr
	}
	if inScope && IsCasualPraiseLike(userText) {
		formal := strOrEmpty(profile.Tone) == "formal"
		out := metaNoLLM(reasonAIGenerated, PathConsulting)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact,
			casualPraiseReply(formal), "ai", payload.InboundMessageID, out)
		return err == nil, err
	}

	// ── In-scope question → LLM path ────────────────────────────────────
	s.resetScopeCounters(ctx, payload.TenantID, convo.ID)

	if IsThirdPartyBuyerLookup(userText) {
		return s.handleThirdPartyBuyerLookupDenied(ctx, ts, payload, convo, channel, contact)
	}
	if IsOrderStatusInquiry(userText) || IsSelfBuyerOrderLookup(userText) || IsOrderRefStatusLookup(userText) {
		return s.handleCustomerOrderStatus(ctx, ts, payload.TenantSchema, payload, convo, channel, contact, userText, history)
	}

	cached, _ := s.getCachedAnswer(ctx, payload.TenantID, userText)
	if cached != "" {
		cached = applyOutputPolicy(cached)
		out := metaNoLLM(reasonAIGenerated, PathFAQCache)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, cached, "ai", payload.InboundMessageID, out)
		return err == nil, err
	}

	kbHybrid, kbTopScore, kbRetrieval := s.retrieveKBHybrid(ctx, payload.TenantID, payload.TenantSchema, userText, ts, kbEntries)
	rlog.Info("AI job: hybrid KB retrieval",
		"selected", len(kbHybrid),
		"total", len(kbEntries),
		"topScore", kbTopScore,
		"shadow", kbRetrieval != nil && kbRetrieval.ShadowOnly,
	)

	// FAQ bypass — no LLM call when KB match is strong (cost optimization).
	mode := appflag.EffectiveRetrievalMode(ctx, payload.TenantID, payload.TenantSchema)
	var rrfScores []retrieval.ScoredEntry
	if kbRetrieval != nil {
		rrfScores = kbRetrieval.Entries
	}
	if direct, ok := tryFAQDirectAnswerHybrid(userText, kbHybrid, rrfScores, kbRetrieval, mode); ok {
		finalReply := applyOutputPolicy(direct)
		s.setCachedAnswer(ctx, payload.TenantID, userText, finalReply)
		out := metaNoLLM(reasonAIGenerated, PathFAQDirect)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, finalReply, "ai", payload.InboundMessageID, out)
		return err == nil, err
	}

	strongFAQMatch := false
	if len(rrfScores) > 0 && FAQDirectGuardsPass(userText) {
		_, strongFAQMatch = retrieval.FAQDirectOK(rrfScores, retrieval.DefaultFAQMinScore, retrieval.DefaultFAQMinMargin)
	}

	planCode, _ := loadSubscriptionPlanCode(ctx, ts)
	complexity := ClassifyComplexity(userText, classifier.Label, kbTopScore, strongFAQMatch)
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
		err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact,
			"Maaf kak, kuota AI bulan ini sudah mencapai batas. Tim kami akan segera menghubungi kakak ya 🙏",
			"system", payload.InboundMessageID, out)
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
	if catCtx := BuildCatalogContext(catalog); catCtx != "" {
		business = business + "\n\n" + catCtx
	}
	kbCtx := BuildKnowledgeContext(kbForPrompt)
	summary, _ := GetLatestSummary(ctx, payload.TenantSchema, convo.ID)
	rlog.Info("AI job: context sizes",
		"sys", len(sys),
		"business", len(business),
		"kb", len(kbCtx),
		"histTurns", len(histForPrompt),
	)

	toolExec := NewCatalogToolExecutor(catalog)
	reply, compUsage, usedTools, err := s.anthropic.GenerateSalesReplyWithCatalogTools(
		ctx,
		route.Model,
		SalesReplyRequest{
			System:   sys,
			Business: business,
			KB:       kbCtx,
			Summary:  summary,
			History:  histForPrompt,
			UserText: userText,
		},
		toolExec,
	)
	if err != nil {
		rlog.Error("AI job: anthropic sales reply failed",
			"err", err,
			"model", route.Model,
			"tenantId", payload.TenantID,
			"convoId", convo.ID,
		)
		fallbackText := applyOutputPolicy(scopeDirectionReply(profile))
		out := metaNoLLM(reasonAIGenerated, PathAutoFallback)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		sendErr := s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, fallbackText, "ai", payload.InboundMessageID, out)
		return sendErr == nil, sendErr
	}

	groundedReply, wasGrounded, groundReason := groundLLMReply(reply, userText, profile, catalog, history,
		func(ut string, p *dbBusinessProfile, c []dbCatalogItem, h []dbMessage) (string, bool) {
			return s.replyFromBusinessCatalogHybrid(ctx, payload.TenantID, payload.TenantSchema, ut, ts, p, c, h)
		})
	if wasGrounded {
		rlog.Warn("AI job: LLM reply grounded to catalog",
			"reason", groundReason,
			"tenantId", payload.TenantID,
			"convoId", convo.ID,
		)
	}

	_, _, newSession := usage.TrackAIExchange(ctx, payload.TenantID, convo.ID)
	tokens := compUsage.InputTokens + compUsage.OutputTokens

	finalReply := applyOutputPolicy(groundedReply)
	llmPath := PathLLM
	if usedTools {
		llmPath = PathLLMTools
	}
	if wasGrounded {
		llmPath = PathLLMGrounded
	}
	out := metaFromRoute(reasonAIGenerated, llmPath, route)
	out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, compUsage.InputTokens, compUsage.OutputTokens)
	err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, finalReply, "ai", payload.InboundMessageID, out)
	if err != nil {
		return false, err
	}

	usage.RecordAITokens(ctx, payload.TenantID, convo.ID, tokens)
	if newSession {
		_ = usage.RecordEvent(ctx, payload.TenantSchema, "ai_conversation", 1, nil)
	}
	if tokens > 0 {
		_ = usage.RecordEvent(ctx, payload.TenantSchema, "ai_token", tokens, nil)
	}
	return true, nil
}

// FallbackAutoReply sends a generic fallback and pauses AI on the conversation.
func (s *AutoReplyService) FallbackAutoReply(ctx context.Context, payload AiReplyJobPayload) error {
	ts, err := openTenantScope(ctx, payload.TenantSchema)
	if err != nil {
		return err
	}

	convo, err := loadConversation(ctx, ts, payload.ConversationID)
	if err != nil || convo == nil {
		return err
	}
	contact, err := loadContact(ctx, ts, convo.ContactID)
	if err != nil {
		return err
	}
	channel, err := loadChannel(ctx, ts, convo.ChannelID)
	if err != nil {
		return err
	}
	if contact == nil || channel == nil || channel.AccessToken == nil || channel.MetaPhoneNumberID == nil {
		return nil
	}

	out := metaNoLLM(reasonNonQuestion, PathAutoFallback)
	out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
	err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact,
		"Maaf kak, saat ini sistem kami sedang sibuk. Tim kami akan bantu balas secepatnya ya 🙏",
		"system", payload.InboundMessageID, out)
	if err != nil {
		return err
	}
	return pauseAI(ctx, ts, convo.ID, "Auto fallback setelah retry AI gagal")
}

func (s *AutoReplyService) handleCustomerOrderCancel(
	ctx context.Context,
	ts tenantScopedQuerier,
	tenantSchema string,
	payload AiReplyJobPayload,
	convo *dbConversation,
	channel *dbChannel,
	contact *dbContact,
	profile *dbBusinessProfile,
	userText string,
) (bool, error) {
	scope := orderAccessScope{ConversationID: convo.ID, ContactID: convo.ContactID}
	res, err := resolvePersistedOrderCancel(ctx, ts, tenantSchema, scope, userText)
	if err != nil {
		rlog.Warn("order cancel: load failed", "err", err, "convoId", convo.ID)
		return false, err
	}
	tone := strOrEmpty(profile.Tone)
	send := func(text string, meta AiReplyMeta) (bool, error) {
		meta.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err := s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, text, "system", payload.InboundMessageID, meta)
		return err == nil, err
	}
	if res.AccessDenied {
		meta := metaNoLLM(reasonNonQuestion, PathOrderCancel)
		meta.OrderAction = "cancel"
		return send(orderAccessDeniedReply(), meta)
	}
	if res.NotFound {
		meta := metaNoLLM(reasonNonQuestion, PathOrderCancel)
		meta.OrderAction = "cancel"
		return send(orderRefNotFoundReply(parseOrderRefFromMessage(userText)), meta)
	}
	if res.NeedPick {
		meta := metaNoLLM(reasonNonQuestion, PathOrderCancel)
		meta.OrderAction = "cancel"
		return send(orderCancelPickRefReply(res.List), meta)
	}
	o := res.Order
	if o == nil {
		meta := metaNoLLM(reasonNonQuestion, PathOrderCancel)
		meta.OrderAction = "cancel"
		return send(orderNoneToCancelReply(), meta)
	}
	if contact != nil && !OrderChatAccessAllowed(o, scope, contact.PhoneNumber, contact.PhoneNumber) {
		meta := metaNoLLM(reasonNonQuestion, PathOrderCancel)
		meta.OrderAction = "cancel"
		return send(orderAccessDeniedReply(), meta)
	}
	ref := FormatOrderNumber(o.ID)
	if strings.EqualFold(o.Status, "cancelled") {
		meta := metaNoLLM(reasonNonQuestion, PathOrderCancel)
		meta.OrderID = o.ID
		meta.OrderAction = "cancel"
		return send(orderAlreadyCancelledReply(ref), meta)
	}
	if !cancellableOrderStatuses[o.Status] {
		meta := metaNoLLM(reasonNonQuestion, PathOrderCancel)
		meta.OrderID = o.ID
		meta.OrderAction = "cancel"
		msg := fmt.Sprintf("Maaf kak, pesanan %s sudah %s dan tidak bisa dibatalkan lewat chat. Tim CS bisa bantu ya.",
			ref, orderStatusLabelID(o.Status))
		return send(msg, meta)
	}
	if err := cancelPersistedOrder(ctx, ts, tenantSchema, o.ID, scope); err != nil {
		if err == sql.ErrNoRows {
			meta := metaNoLLM(reasonNonQuestion, PathOrderCancel)
			meta.OrderID = o.ID
			meta.OrderAction = "cancel"
			return send(orderAlreadyCancelledReply(ref), meta)
		}
		rlog.Warn("order cancel: update failed", "err", err, "orderId", o.ID)
		return false, err
	}
	rlog.Info("order cancelled by customer via chat", "orderId", o.ID, "convoId", convo.ID, "ref", ref)
	meta := metaNoLLM(reasonNonQuestion, PathOrderCancel)
	meta.OrderID = o.ID
	meta.OrderAction = "cancel"
	return send(orderCancelCustomerReply(tone, ref), meta)
}

func (s *AutoReplyService) handleThirdPartyBuyerLookupDenied(
	ctx context.Context,
	ts tenantScopedQuerier,
	payload AiReplyJobPayload,
	convo *dbConversation,
	channel *dbChannel,
	contact *dbContact,
) (bool, error) {
	meta := metaNoLLM(reasonOutOfScope, PathOrderLookupDenied)
	meta.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
	err := s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact,
		thirdPartyBuyerLookupDeniedReply(), "system", payload.InboundMessageID, meta)
	return err == nil, err
}

func (s *AutoReplyService) handleCustomerOrderStatus(
	ctx context.Context,
	ts tenantScopedQuerier,
	tenantSchema string,
	payload AiReplyJobPayload,
	convo *dbConversation,
	channel *dbChannel,
	contact *dbContact,
	userText string,
	history []dbMessage,
) (bool, error) {
	scope := orderAccessScope{ConversationID: convo.ID, ContactID: convo.ContactID}
	res, err := resolvePersistedOrderStatus(ctx, ts, tenantSchema, scope, userText, history)
	if err != nil {
		return false, err
	}
	meta := metaNoLLM(reasonAIGenerated, PathOrderStatus)
	meta.OrderAction = "status"
	send := func(text string) (bool, error) {
		meta.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err := s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, text, "system", payload.InboundMessageID, meta)
		return err == nil, err
	}
	if res.AccessDenied {
		return send(orderAccessDeniedReply())
	}
	if res.NotFound {
		return send(orderRefNotFoundReply(parseOrderRefFromMessage(userText)))
	}
	if res.RecipientHintNotFound {
		return send(orderRecipientHintNotFoundReply())
	}
	if res.ActiveOnly && res.Order == nil {
		return send(orderNoActiveOrdersReply())
	}
	if res.NeedPick {
		return send(orderStatusPickRefReply(res.List))
	}
	o := res.Order
	if o == nil {
		if orderSt, err := s.getOrderState(ctx, payload.TenantID, convo.ID); err == nil && orderSt != nil && orderSt.ProductComplete() {
			body := formatOrderSummary(*orderSt)
			if body != "" {
				body += "\n\n(Ini draft pesanan dari chat — belum dikonfirmasi ke toko.)"
				return send(body)
			}
		}
		return send(orderNoneFoundReply())
	}
	if contact != nil && !OrderChatAccessAllowed(o, scope, contact.PhoneNumber, contact.PhoneNumber) {
		return send(orderAccessDeniedReply())
	}
	meta.OrderID = o.ID
	body := formatPersistedOrderSummary(o)
	if strings.EqualFold(o.Status, "cancelled") {
		body += "\n\nPesanan ini sudah dibatalkan."
	} else if cancellableOrderStatuses[o.Status] {
		body += "\n\nUntuk membatalkan, ketik saja: batalkan pesanan."
	}
	return send(body)
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
			!mentionsOrderQty(text) && !orderSizeLineMatches(text)
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

type scoredKB struct {
	entry dbKBEntry
	score float64
}

func scoreKBEntries(query string, kb []dbKBEntry) []scoredKB {
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
	for i := 1; i < len(scored); i++ {
		for j := i; j > 0 && scored[j].score > scored[j-1].score; j-- {
			scored[j], scored[j-1] = scored[j-1], scored[j]
		}
	}
	return scored
}

func retrieveHybridKB(query string, kb []dbKBEntry) []dbKBEntry {
	scored := scoreKBEntries(query, kb)
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
		cleaned = strutil.TruncateUTF8(cleaned, maxReplyLen)
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

func loadSubscriptionPlanCode(ctx context.Context, ts tenantScopedQuerier) (string, error) {
	var planCode string
	var isTrial bool
	err := ts.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT plan_code, COALESCE(is_trial, false) FROM %s
		WHERE status = 'active'
		ORDER BY created_at DESC LIMIT 1`, ts.T("subscription")),
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

func loadConversation(ctx context.Context, ts tenantScopedQuerier, id string) (*dbConversation, error) {
	row := ts.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT id, contact_id, channel_id, ai_handled, ai_paused_at,
		       handoff_reason, last_message_at, last_message_preview, status
		FROM %s WHERE id = $1`, ts.T("conversation")), id)
	c := &dbConversation{}
	err := row.Scan(&c.ID, &c.ContactID, &c.ChannelID, &c.AIHandled,
		&c.AIPausedAt, &c.HandoffReason, &c.LastMessageAt, &c.LastPreview, &c.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

func loadMessage(ctx context.Context, ts tenantScopedQuerier, id string) (*dbMessage, error) {
	row := ts.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT id, direction, author, type, COALESCE(body,''), created_at
		FROM %s WHERE id = $1`, ts.T("message")), id)
	m := &dbMessage{}
	err := row.Scan(&m.ID, &m.Direction, &m.Author, &m.Type, &m.Body, &m.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return m, err
}

func loadBusinessProfile(ctx context.Context, ts tenantScopedQuerier) (*dbBusinessProfile, error) {
	row := ts.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT business_name, description, address, opening_hours,
		       products_services, base_pricing, delivery_area,
		       greeting_template, tone, ai_enabled,
		       COALESCE(catalog_website_url, '')
		FROM %s ORDER BY created_at ASC LIMIT 1`, ts.T("business_profile")))
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

func loadContact(ctx context.Context, ts tenantScopedQuerier, id string) (*dbContact, error) {
	c, err := loadContactPII(ctx, ts, id)
	if err != nil && isMissingPIIColumn(err) {
		return loadContactLegacy(ctx, ts, id)
	}
	return c, err
}

func loadContactPII(ctx context.Context, ts tenantScopedQuerier, id string) (*dbContact, error) {
	row := ts.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT id,
		       COALESCE(phone_number_enc, ''), COALESCE(phone_number, ''),
		       COALESCE(display_name_enc, ''), COALESCE(display_name, '')
		FROM %s WHERE id = $1`, ts.T("contact")), id)
	var c dbContact
	var phoneEnc, phoneLegacy, displayEnc, displayLegacy string
	err := row.Scan(&c.ID, &phoneEnc, &phoneLegacy, &displayEnc, &displayLegacy)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(secrets.DataEncryptionKey)
	phone, err := pii.DecryptOrLegacy(phoneEnc, phoneLegacy, key)
	if err != nil {
		return nil, err
	}
	c.PhoneNumber = phone
	if name, err := pii.DecryptOrLegacy(displayEnc, displayLegacy, key); err != nil {
		return nil, err
	} else if name != "" && name != pii.Placeholder {
		c.DisplayName = &name
	}
	return &c, nil
}

func loadContactLegacy(ctx context.Context, ts tenantScopedQuerier, id string) (*dbContact, error) {
	row := ts.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT id, phone_number, display_name
		FROM %s WHERE id = $1`, ts.T("contact")), id)
	c := &dbContact{}
	err := row.Scan(&c.ID, &c.PhoneNumber, &c.DisplayName)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

func isMissingPIIColumn(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "does not exist") && strings.Contains(msg, "column")
}

func loadChannel(ctx context.Context, ts tenantScopedQuerier, id string) (*dbChannel, error) {
	row := ts.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT id, provider, status,
		       COALESCE(access_token_enc, ''), COALESCE(access_token, ''),
		       meta_phone_number_id, meta_waba_id,
		       COALESCE(display_name_enc, ''), COALESCE(display_name, ''),
		       COALESCE(phone_number_enc, ''), COALESCE(phone_number, '')
		FROM %s WHERE id = $1`, ts.T("whatsapp_channel")), id)
	ch := &dbChannel{}
	var tokenEnc, tokenLegacy, displayEnc, displayLegacy, phoneEnc, phoneLegacy string
	err := row.Scan(&ch.ID, &ch.Provider, &ch.Status, &tokenEnc, &tokenLegacy,
		&ch.MetaPhoneNumberID, &ch.MetaWabaID,
		&displayEnc, &displayLegacy, &phoneEnc, &phoneLegacy)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(secrets.DataEncryptionKey)
	token, err := pii.DecryptOrLegacy(tokenEnc, tokenLegacy, key)
	if err != nil {
		return nil, err
	}
	if token != "" {
		ch.AccessToken = &token
	}
	display, err := whatsappchannel.DecryptDisplay(displayEnc, displayLegacy, phoneEnc, phoneLegacy, key)
	if err != nil {
		return nil, err
	}
	ch.DisplayName = display.DisplayName
	ch.PhoneNumber = display.PhoneNumber
	return ch, nil
}

func loadHistory(ctx context.Context, ts tenantScopedQuerier, convoID string, limit int) ([]dbMessage, error) {
	rows, err := ts.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, direction, author, type, COALESCE(body,''), created_at
		FROM %s WHERE conversation_id = $1
		ORDER BY created_at DESC LIMIT $2`, ts.T("message")), convoID, limit)
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

func loadKBEntries(ctx context.Context, ts tenantScopedQuerier, limit int) ([]dbKBEntry, error) {
	rows, err := ts.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, question, answer, category, is_active
		FROM %s
		WHERE is_active = true
		ORDER BY created_at DESC LIMIT $1`, ts.T("knowledge_base_entry")), limit)
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
	ts tenantScopedQuerier,
	tenantID string,
	convo *dbConversation,
	channel *dbChannel,
	contact *dbContact,
	text, author string,
	inboundMessageID string,
	meta AiReplyMeta,
) error {
	meta = metaForSend(meta, inboundMessageID)

	rlog.Info("AI job: sending WhatsApp text",
		"len", len(text),
		"author", author,
		"path", meta.Path,
		"reason", meta.Reason,
		"llmUsed", meta.LLMUsed,
		"model", meta.Model,
		"tier", meta.Tier,
		"inboundId", inboundMessageID,
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
	if len(author) > 10 {
		return fmt.Errorf("invalid message author %q (max 10 chars) — check sendAiMessage argument order", author)
	}
	contactPhone := whatsapp.NormalizeRecipient(contact.PhoneNumber)
	if contactPhone == "" {
		return fmt.Errorf("invalid contact phone")
	}

	if inboundMessageID != "" {
		if s.aiSendAlreadyDone(ctx, inboundMessageID) {
			rlog.Info("AI job: skip duplicate send (redis done key)", "inboundId", inboundMessageID)
			return nil
		}
	}

	draft, found, err := s.findOutboundForInbound(ctx, ts, inboundMessageID)
	if err != nil {
		return fmt.Errorf("lookup outbound draft: %w", err)
	}
	if found && draft.ExternalID != "" {
		rlog.Info("AI job: skip duplicate send (already persisted)", "inboundId", inboundMessageID, "msgId", draft.ID)
		s.markAiSendDone(ctx, inboundMessageID)
		return nil
	}

	var msgID string
	var msgCreatedAt time.Time
	if found {
		msgID = draft.ID
	} else {
		msgID, msgCreatedAt, err = s.insertPendingOutbound(ctx, ts, convo.ID, author, text, meta)
		if err != nil {
			rlog.Error("AI job: failed inserting pending outbound", "err", err)
			return fmt.Errorf("insert message: %w", err)
		}
	}

	extID, err := whatsapp.SendText(
		ctx,
		*channel.AccessToken,
		*channel.MetaPhoneNumberID,
		contactPhone,
		text,
	)
	if err != nil {
		s.markOutboundFailed(ctx, ts, msgID)
		rlog.Error("AI job: meta SendText failed",
			"err", err,
			"convoId", convo.ID,
			"channelId", channel.ID,
			"msgId", msgID,
		)
		return fmt.Errorf("send whatsapp: %w", err)
	}

	preview := strutil.TruncateUTF8(text, 280)
	if msgCreatedAt.IsZero() {
		msgCreatedAt = time.Now().UTC()
	}
	if err := s.finalizeOutbound(ctx, ts, tenantID, convo.ID, msgID, extID, preview, msgCreatedAt); err != nil {
		rlog.Error("AI job: failed finalizing outbound", "err", err, "msgId", msgID)
		return err
	}

	s.markAiSendDone(ctx, inboundMessageID)
	return nil
}

func pauseAI(ctx context.Context, ts tenantScopedQuerier, convoID, reason string) error {
	_, err := ts.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s
		SET ai_handled = false, ai_paused_at = NOW(), handoff_reason = $1
		WHERE id = $2`, ts.T("conversation")),
		reason, convoID,
	)
	return err
}
