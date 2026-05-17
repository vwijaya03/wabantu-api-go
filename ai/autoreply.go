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
		rlog.Warn("AI job: business profile incomplete, using CS default response")
		err = s.sendAiMessage(ctx, db, convo, channel, contact,
			nonAiDefaultReply(profile), "system", reasonProfileIncomplete)
		return err == nil, err
	}

	userText := SanitizeForPrompt(inbound.Body)
	rlog.Info("AI job: inbound text", "lenUserText", len(userText))

	if IsGreetingLike(userText) {
		greet := strOrEmpty(profile.GreetingTemplate)
		if greet == "" {
			if strOrEmpty(profile.Tone) == "formal" {
				greet = "Selamat siang, kak. Ada yang bisa kami bantu?"
			} else {
				greet = "Selamat siang kak! Ada yang bisa aku bantu?"
			}
		}
		err = s.sendAiMessage(ctx, db, convo, channel, contact, greet, "ai", reasonNonQuestion)
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
		err = s.sendAiMessage(ctx, db, convo, channel, contact, text, "system", reasonOutOfScope)
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

	fallbackKW := []string{"harga", "stok", "produk", "order", "pengiriman", "ukuran", "size"}
	inScope := IsWithinBusinessScope(userText, scopeKeywords, fallbackKW)
	classifier := classifyMessage(userText, inScope, profile)
	rlog.Info("AI job: classifier",
		"label", classifier.Label,
		"confidence", classifier.Confidence,
	)

	// ── Handle: sensitive escalation ─────────────────────────────────────
	if classifier.Label == "sensitive_escalate" {
		err = s.sendAiMessage(ctx, db, convo, channel, contact,
			"Maaf kak, untuk topik ini tim CS kami akan langsung mengambil alih dan segera menghubungi kakak 🙏",
			"system", reasonOutOfScope)
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
		err = s.sendAiMessage(ctx, db, convo, channel, contact, text, "system", reasonOutOfScope)
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
		err = s.sendAiMessage(ctx, db, convo, channel, contact, text, "system", reasonNonQuestion)
		return err == nil, err
	}

	// ── Handle: low-confidence question ──────────────────────────────────
	if classifier.Label == "in_scope_question" && classifier.Confidence < llmConfidenceThreshold {
		err = s.sendAiMessage(ctx, db, convo, channel, contact,
			scopeDirectionReply(profile), "system", reasonNonQuestion)
		return err == nil, err
	}

	// ── Handle: order intent state machine ───────────────────────────────
	if classifier.Label == "order_intent" {
		sent, oErr := s.handleOrderFlow(ctx, db, payload.TenantID, convo, channel, contact, userText, profile)
		return sent, oErr
	}

	// ── In-scope question → LLM path ────────────────────────────────────
	s.resetScopeCounters(ctx, payload.TenantID, convo.ID)

	cached, _ := s.getCachedAnswer(ctx, payload.TenantID, userText)
	if cached != "" {
		rlog.Info("AI job: using cached canonical answer")
		err = s.sendAiMessage(ctx, db, convo, channel, contact, cached, "ai", reasonAIGenerated)
		return err == nil, err
	}

	kbHybrid := retrieveHybridKB(userText, kbEntries)
	rlog.Info("AI job: hybrid retrieval",
		"selected", len(kbHybrid),
		"total", len(kbEntries),
	)

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
	histCtx := BuildConversationContext(histForPrompt)
	rlog.Info("AI job: context sizes",
		"sys", len(sys),
		"business", len(business),
		"kb", len(kbCtx),
		"hist", len(histCtx),
	)

	reply, err := s.anthropic.GenerateReply(ctx, sys, business, kbCtx, histCtx, userText)
	if err != nil {
		rlog.Error("AI job: anthropic.GenerateReply failed",
			"err", err,
			"tenantId", payload.TenantID,
			"convoId", convo.ID,
		)
		return false, err
	}

	finalReply := applyOutputPolicy(reply)
	s.setCachedAnswer(ctx, payload.TenantID, userText, finalReply)
	err = s.sendAiMessage(ctx, db, convo, channel, contact, finalReply, "ai", reasonAIGenerated)
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

	err = s.sendAiMessage(ctx, db, convo, channel, contact,
		"Maaf kak, saat ini sistem kami sedang sibuk. Tim kami akan bantu balas secepatnya ya 🙏",
		"system", reasonNonQuestion)
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
	tenantID string,
	convo *dbConversation,
	channel *dbChannel,
	contact *dbContact,
	userText string,
	profile *dbBusinessProfile,
) (bool, error) {
	state, _ := s.getOrderState(ctx, tenantID, convo.ID)

	if state == nil {
		s.setOrderState(ctx, tenantID, convo.ID, orderState{Step: "ask_product"})
		err := s.sendAiMessage(ctx, db, convo, channel, contact,
			"Siap kak, mau order produk yang mana ya? Sekalian tulis varian/size kalau ada.",
			"system", reasonNonQuestion)
		return err == nil, err
	}

	switch state.Step {
	case "ask_product":
		product := userText
		if len(product) > 120 {
			product = product[:120]
		}
		s.setOrderState(ctx, tenantID, convo.ID, orderState{Step: "ask_variant", Product: product})
		err := s.sendAiMessage(ctx, db, convo, channel, contact,
			"Baik kak. Untuk variannya apa ya (mis. size/warna)?",
			"system", reasonNonQuestion)
		return err == nil, err

	case "ask_variant":
		variant := userText
		if m := sizeRe.FindString(userText); m != "" {
			variant = m
		} else if len(variant) > 60 {
			variant = variant[:60]
		}
		s.setOrderState(ctx, tenantID, convo.ID, orderState{
			Step: "ask_qty", Product: state.Product, Variant: variant,
		})
		err := s.sendAiMessage(ctx, db, convo, channel, contact,
			"Siap kak. Mau pesan berapa pcs?",
			"system", reasonNonQuestion)
		return err == nil, err

	case "ask_qty":
		qty := 0
		if m := qtyRe.FindStringSubmatch(userText); len(m) > 1 {
			fmt.Sscanf(m[1], "%d", &qty)
		}
		s.setOrderState(ctx, tenantID, convo.ID, orderState{
			Step: "ask_address", Product: state.Product, Variant: state.Variant, Qty: qty,
		})
		err := s.sendAiMessage(ctx, db, convo, channel, contact,
			"Terima kasih kak. Boleh kirim alamat pengiriman lengkapnya ya.",
			"system", reasonNonQuestion)
		return err == nil, err

	case "ask_address":
		if addrRe.MatchString(userText) {
			s.clearOrderState(ctx, tenantID, convo.ID)
			err := s.sendAiMessage(ctx, db, convo, channel, contact,
				"Sip kak, datanya sudah lengkap. Tim CS kami akan segera konfirmasi order kakak ya 🙏",
				"system", reasonNonQuestion)
			return err == nil, err
		}
	}

	err := s.sendAiMessage(ctx, db, convo, channel, contact,
		"Boleh kak lanjutkan data ordernya, nanti tim CS bantu proses sampai selesai ya.",
		"system", reasonNonQuestion)
	return err == nil, err
}

// ─── Message classifier ──────────────────────────────────────────────────────

var orderKeywords = []string{"order", "pesan", "beli", "checkout", "jadi ambil", "jadi beli"}
var sensitiveKeywords = []string{
	"penipuan", "fraud", "komplain keras", "lapor polisi",
	"ancam", "refund gagal", "tagihan salah",
}

func classifyMessage(userText string, inScope bool, _ *dbBusinessProfile) classifyResult {
	text := strings.ToLower(userText)

	for _, kw := range sensitiveKeywords {
		if strings.Contains(text, kw) {
			return classifyResult{Label: "sensitive_escalate", Confidence: 0.98}
		}
	}
	if !inScope {
		return classifyResult{Label: "out_of_scope", Confidence: 0.9}
	}
	for _, kw := range orderKeywords {
		if strings.Contains(text, kw) {
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

func loadConversation(ctx context.Context, db *sql.DB, id string) (*dbConversation, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, contact_id, channel_id, ai_handled, ai_paused_at,
		       handoff_reason, last_message_at, last_message_preview, status
		FROM conversations WHERE id = $1`, id)
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
		FROM messages WHERE id = $1`, id)
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
		       COALESCE(catalog_url, '')
		FROM business_profiles ORDER BY created_at ASC LIMIT 1`)
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
		FROM contacts WHERE id = $1`, id)
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
		FROM whatsapp_channels WHERE id = $1`, id)
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
		FROM messages WHERE conversation_id = $1
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
		FROM knowledge_base_entries
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
	convo *dbConversation,
	channel *dbChannel,
	contact *dbContact,
	text, author, reason string,
) error {
	rlog.Info("AI job: sending WhatsApp text",
		"len", len(text),
		"author", author,
		"reason", reason,
	)

	// TODO: call Meta Cloud API to actually send the WhatsApp message.
	// For now we persist the message and assume external delivery is handled
	// by a separate WhatsApp provider package (to be wired in Phase 2).
	externalID := fmt.Sprintf("ai-%s-%d", convo.ID[:8], time.Now().UnixMilli())

	preview := text
	if len(preview) > 280 {
		preview = preview[:280]
	}

	metadataJSON := fmt.Sprintf(`{"reason":"%s"}`, reason)
	var msgCreatedAt time.Time
	err := db.QueryRowContext(ctx, `
		INSERT INTO messages (conversation_id, external_id, direction, author, type, body, metadata, status)
		VALUES ($1, $2, 'out', $3, 'text', $4, $5, 'sent')
		RETURNING created_at`,
		convo.ID, externalID, author, text, metadataJSON,
	).Scan(&msgCreatedAt)
	if err != nil {
		rlog.Error("AI job: failed inserting outbound message", "err", err)
		return fmt.Errorf("insert message: %w", err)
	}

	_, err = db.ExecContext(ctx, `
		UPDATE conversations
		SET last_message_at = $1, last_message_preview = $2, status = 'open'
		WHERE id = $3`,
		msgCreatedAt, preview, convo.ID,
	)
	if err != nil {
		rlog.Error("AI job: failed updating conversation", "err", err)
		return fmt.Errorf("update conversation: %w", err)
	}
	return nil
}

func pauseAI(ctx context.Context, db *sql.DB, convoID, reason string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE conversations
		SET ai_handled = false, ai_paused_at = NOW(), handoff_reason = $1
		WHERE id = $2`,
		reason, convoID,
	)
	return err
}
