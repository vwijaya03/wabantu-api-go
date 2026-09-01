package business

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"encore.dev/rlog"

	appauth "encore.app/wabantu/auth"
	"encore.app/wabantu/ai"
	"encore.app/wabantu/kb"
	apperr "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/usage"
)

const (
	setupInterviewStagingTTL = 24 * time.Hour
	setupInterviewMaxTurns   = 30
	setupInterviewMaxFAQ     = 20
	setupInterviewMaxMsgLen  = 2000
)

type setupInterviewMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type setupInterviewSession struct {
	SessionID      string                  `json:"sessionId"`
	TenantSchema   string                  `json:"tenantSchema"`
	UploadedBy     string                  `json:"uploadedBy"`
	Phase          string                  `json:"phase"`
	Messages       []setupInterviewMessage `json:"messages"`
	ProfileDraft   ImportFieldSet          `json:"profileDraft"`
	Tone           *string                 `json:"tone,omitempty"`
	GreetingDraft  *string                 `json:"greetingDraft,omitempty"`
	FAQDrafts      []interviewFAQDraft     `json:"faqDrafts"`
	ReadyForReview bool                    `json:"readyForReview"`
	InputTokens    int                     `json:"inputTokens"`
	OutputTokens   int                     `json:"outputTokens"`
	TurnCount      int                     `json:"turnCount"`
	CreatedAt      time.Time               `json:"createdAt"`
}

type SetupInterviewStartResponse struct {
	SessionID           string                  `json:"sessionId"`
	Phase               string                  `json:"phase"`
	Messages            []setupInterviewMessage `json:"messages"`
	ProfileDraft        ImportFieldSet          `json:"profileDraft"`
	FAQDrafts           []interviewFAQDraft     `json:"faqDrafts"`
	ReadyForReview      bool                    `json:"readyForReview"`
	TokenQuotaRemaining int                     `json:"tokenQuotaRemaining"`
	TokenQuotaLimit     int                     `json:"tokenQuotaLimit"`
	QuotaNotice         string                  `json:"quotaNotice"`
}

type SetupInterviewMessageRequest struct {
	Message string `json:"message"`
}

type SetupInterviewMessageResponse struct {
	SessionID           string                  `json:"sessionId"`
	Phase               string                  `json:"phase"`
	Messages            []setupInterviewMessage `json:"messages"`
	ProfileDraft        ImportFieldSet          `json:"profileDraft"`
	FAQDrafts           []interviewFAQDraft     `json:"faqDrafts"`
	ReadyForReview      bool                    `json:"readyForReview"`
	TokensUsed          int                     `json:"tokensUsed"`
	TokenQuotaRemaining int                     `json:"tokenQuotaRemaining"`
	TokenQuotaLimit     int                     `json:"tokenQuotaLimit"`
	QuotaNotice         string                  `json:"quotaNotice"`
}

type SetupInterviewGetResponse struct {
	SessionID           string                  `json:"sessionId"`
	Phase               string                  `json:"phase"`
	Messages            []setupInterviewMessage `json:"messages"`
	ProfileDraft        ImportFieldSet          `json:"profileDraft"`
	FAQDrafts           []interviewFAQDraft     `json:"faqDrafts"`
	ReadyForReview      bool                    `json:"readyForReview"`
	TokenQuotaRemaining int                     `json:"tokenQuotaRemaining"`
	TokenQuotaLimit     int                     `json:"tokenQuotaLimit"`
	QuotaNotice         string                  `json:"quotaNotice"`
}

type SetupInterviewPublishFAQItem struct {
	Question string  `json:"question"`
	Answer   string  `json:"answer"`
	Category *string `json:"category,omitempty"`
	Include  bool    `json:"include"`
}

type SetupInterviewPublishRequest struct {
	Profile *UpdateProfileRequest            `json:"profile,omitempty"`
	FAQ     []SetupInterviewPublishFAQItem   `json:"faq"`
}

type SetupInterviewPublishResponse struct {
	ProfileUpdated bool   `json:"profileUpdated"`
	FAQPublished   int    `json:"faqPublished"`
	FAQSkipped     int    `json:"faqSkipped"`
	Message        string `json:"message"`
}

func setupInterviewStagingKey(sessionID string) string {
	return "setup:interview:" + sessionID
}

func saveSetupInterviewSession(ctx context.Context, session *setupInterviewSession) error {
	raw, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return appauth.RedisClient().Set(ctx, setupInterviewStagingKey(session.SessionID), raw, setupInterviewStagingTTL).Err()
}

func loadSetupInterviewSession(ctx context.Context, sessionID, tenantSchema string) (*setupInterviewSession, error) {
	raw, err := appauth.RedisClient().Get(ctx, setupInterviewStagingKey(sessionID)).Bytes()
	if err != nil {
		return nil, apperr.NotFound("sesi setup tidak ditemukan atau sudah kedaluwarsa")
	}
	var session setupInterviewSession
	if err := json.Unmarshal(raw, &session); err != nil {
		return nil, apperr.Internal("sesi setup rusak")
	}
	if session.TenantSchema != tenantSchema {
		return nil, apperr.Forbidden("sesi setup tidak valid")
	}
	return &session, nil
}

func sessionToStartResponse(session *setupInterviewSession, rem, lim int) *SetupInterviewStartResponse {
	g := sessionToGetResponse(session, rem, lim)
	return &SetupInterviewStartResponse{
		SessionID:           g.SessionID,
		Phase:               g.Phase,
		Messages:            g.Messages,
		ProfileDraft:        g.ProfileDraft,
		FAQDrafts:           g.FAQDrafts,
		ReadyForReview:      g.ReadyForReview,
		TokenQuotaRemaining: g.TokenQuotaRemaining,
		TokenQuotaLimit:     g.TokenQuotaLimit,
		QuotaNotice:         g.QuotaNotice,
	}
}

func sessionToGetResponse(session *setupInterviewSession, rem, lim int) *SetupInterviewGetResponse {
	faq := session.FAQDrafts
	if faq == nil {
		faq = []interviewFAQDraft{}
	}
	msgs := session.Messages
	if msgs == nil {
		msgs = []setupInterviewMessage{}
	}
	return &SetupInterviewGetResponse{
		SessionID:           session.SessionID,
		Phase:               session.Phase,
		Messages:            msgs,
		ProfileDraft:        session.ProfileDraft,
		FAQDrafts:           faq,
		ReadyForReview:      session.ReadyForReview,
		TokenQuotaRemaining: rem,
		TokenQuotaLimit:     lim,
		QuotaNotice:         aiTokenQuotaNotice(rem, lim),
	}
}

//encore:api auth method=POST path=/api/v1/business/setup-interview/start tag:owner
func StartSetupInterview(ctx context.Context) (*SetupInterviewStartResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if !user.CanPerformOwnerActions() {
		return nil, apperr.Forbidden("hanya owner atau admin platform (saat pantau tenant) yang bisa setup interview")
	}

	_, rem, lim := usage.CheckQuota(ctx, user.TenantSchema, "ai_token")

	profResp, err := GetProfile(ctx)
	if err != nil {
		return nil, err
	}
	p := profResp.Profile

	sessionID := fmt.Sprintf("setup_%d", time.Now().UnixNano())
	opening := initialSetupMessage(p)
	phase := "profile"
	if profileFieldsComplete(profileDraftFromResponse(p)) {
		phase = "faq"
	}

	session := &setupInterviewSession{
		SessionID:    sessionID,
		TenantSchema: user.TenantSchema,
		UploadedBy:   user.AccountID,
		Phase:        phase,
		Messages: []setupInterviewMessage{
			{Role: "assistant", Content: opening},
		},
		ProfileDraft: profileDraftFromResponse(p),
		FAQDrafts:    []interviewFAQDraft{},
		CreatedAt:    time.Now(),
	}
	if p.Tone != nil {
		session.Tone = p.Tone
	}
	if p.GreetingTemplate != nil {
		session.GreetingDraft = p.GreetingTemplate
	}

	if err := saveSetupInterviewSession(ctx, session); err != nil {
		return nil, apperr.Internal("gagal membuat sesi setup")
	}
	return sessionToStartResponse(session, rem, lim), nil
}

//encore:api auth method=GET path=/api/v1/business/setup-interview/session/:sessionId tag:owner
func GetSetupInterviewSession(ctx context.Context, sessionId string) (*SetupInterviewGetResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if !user.CanPerformOwnerActions() {
		return nil, apperr.Forbidden("hanya owner atau admin platform (saat pantau tenant) yang bisa setup interview")
	}
	session, err := loadSetupInterviewSession(ctx, sessionId, user.TenantSchema)
	if err != nil {
		return nil, err
	}
	_, rem, lim := usage.CheckQuota(ctx, user.TenantSchema, "ai_token")
	return sessionToGetResponse(session, rem, lim), nil
}

//encore:api auth method=POST path=/api/v1/business/setup-interview/session/:sessionId/message tag:owner
func SendSetupInterviewMessage(ctx context.Context, sessionId string, req *SetupInterviewMessageRequest) (*SetupInterviewMessageResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if !user.CanPerformOwnerActions() {
		return nil, apperr.Forbidden("hanya owner atau admin platform (saat pantau tenant) yang bisa setup interview")
	}
	if req == nil {
		return nil, apperr.BadRequest("message wajib diisi")
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		return nil, apperr.BadRequest("message wajib diisi")
	}
	if len(msg) > setupInterviewMaxMsgLen {
		return nil, apperr.BadRequest("pesan terlalu panjang")
	}

	ok, _, lim := usage.CheckQuota(ctx, user.TenantSchema, "ai_token")
	if !ok && lim > 0 {
		return nil, apperr.BadRequest("kuota token AI bulan ini habis")
	}

	session, err := loadSetupInterviewSession(ctx, sessionId, user.TenantSchema)
	if err != nil {
		return nil, err
	}
	if session.TurnCount >= setupInterviewMaxTurns {
		return nil, apperr.BadRequest("sesi sudah mencapai batas percakapan — lanjut ke review dan publish")
	}

	session.Messages = append(session.Messages, setupInterviewMessage{Role: "user", Content: msg})
	session.TurnCount++

	apiKey := strings.TrimSpace(secrets.AnthropicAPIKey)
	if apiKey == "" {
		return nil, apperr.Internal("AI belum dikonfigurasi")
	}
	client := ai.NewAnthropicClient(apiKey, ai.AnthropicConfig{
		Model:     resolveInterviewModel(),
		MaxTokens: 1024,
	})

	userPrompt := buildInterviewUserPrompt(session, msg)
	raw, compUsage, err := client.CompleteText(ctx, resolveInterviewModel(), setupInterviewSystemPrompt, userPrompt, 1024)
	if err != nil {
		return nil, apperr.Internal("AI setup gagal: " + err.Error())
	}

	turn, err := parseInterviewTurn(raw)
	if err != nil {
		rlog.Warn("setup interview parse failed", "err", err, "rawPreview", previewSetupRaw(raw))
		turn = aiInterviewTurn{
			AssistantMessage: "Maaf, bisa ulangi jawabannya dengan lebih singkat? Saya hanya perlu info tentang toko dan kebijakan umum ya.",
			Phase:            session.Phase,
		}
	}

	session.InputTokens += compUsage.InputTokens
	session.OutputTokens += compUsage.OutputTokens
	tokens := compUsage.InputTokens + compUsage.OutputTokens
	if tokens > 0 {
		_ = usage.RecordEvent(ctx, user.TenantSchema, "ai_token", tokens, nil)
		_ = usage.RecordAIActivity(ctx, usage.AIActivityParams{
			TenantSchema: user.TenantSchema,
			TenantID:     user.TenantID,
			Purpose:      "setup_interview",
			Path:         "setup_interview_message",
			Reason:       "owner_wizard_turn",
			Model:        resolveInterviewModel(),
			Tier:         "haiku",
			LLMUsed:      true,
			InputTokens:  compUsage.InputTokens,
			OutputTokens: compUsage.OutputTokens,
		})
	}

	if turn.ProfileUpdates != nil {
		mergeProfileDraft(&session.ProfileDraft, turn.ProfileUpdates)
	}
	if turn.Tone != nil {
		t := strings.TrimSpace(*turn.Tone)
		if t == "formal" || t == "casual" || t == "friendly" {
			session.Tone = &t
		}
	}

	for _, draft := range turn.FAQAdd {
		if len(session.FAQDrafts) >= setupInterviewMaxFAQ {
			break
		}
		if err := validateFAQDraft(draft.Question, draft.Answer); err != nil {
			rlog.Info("setup interview skip invalid faq", "err", err)
			continue
		}
		draft.Include = true
		if faqDraftExists(session.FAQDrafts, draft.Question) {
			continue
		}
		session.FAQDrafts = append(session.FAQDrafts, draft)
	}

	session.Phase = turn.Phase
	if session.Phase == "profile" && profileFieldsComplete(session.ProfileDraft) {
		session.Phase = "faq"
	}
	session.ReadyForReview = turn.ReadyForReview
	if session.Phase == "review" {
		session.ReadyForReview = true
	}
	if strings.Contains(strings.ToLower(msg), "cukup") || strings.Contains(strings.ToLower(msg), "review") {
		session.ReadyForReview = true
		session.Phase = "review"
	}

	session.Messages = append(session.Messages, setupInterviewMessage{
		Role:    "assistant",
		Content: turn.AssistantMessage,
	})

	if err := saveSetupInterviewSession(ctx, session); err != nil {
		return nil, apperr.Internal("gagal menyimpan sesi")
	}

	tokensUsed := compUsage.InputTokens + compUsage.OutputTokens
	_, remAfter, _ := usage.CheckQuota(ctx, user.TenantSchema, "ai_token")

	start := sessionToStartResponse(session, remAfter, lim)
	return &SetupInterviewMessageResponse{
		SessionID:           start.SessionID,
		Phase:               start.Phase,
		Messages:            start.Messages,
		ProfileDraft:        start.ProfileDraft,
		FAQDrafts:           start.FAQDrafts,
		ReadyForReview:      start.ReadyForReview,
		TokensUsed:          tokensUsed,
		TokenQuotaRemaining: remAfter,
		TokenQuotaLimit:     lim,
		QuotaNotice:         aiTokenQuotaNotice(remAfter, lim),
	}, nil
}

func faqDraftExists(items []interviewFAQDraft, question string) bool {
	q := strings.ToLower(strings.TrimSpace(question))
	for _, it := range items {
		if strings.ToLower(strings.TrimSpace(it.Question)) == q {
			return true
		}
	}
	return false
}

func previewSetupRaw(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) > 120 {
		return raw[:120] + "..."
	}
	return raw
}

//encore:api auth method=POST path=/api/v1/business/setup-interview/session/:sessionId/publish tag:owner
func PublishSetupInterview(ctx context.Context, sessionId string, req *SetupInterviewPublishRequest) (*SetupInterviewPublishResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if !user.CanPerformOwnerActions() {
		return nil, apperr.Forbidden("hanya owner atau admin platform (saat pantau tenant) yang bisa publish setup")
	}
	session, err := loadSetupInterviewSession(ctx, sessionId, user.TenantSchema)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, apperr.BadRequest("body publish wajib")
	}

	updateReq := buildUpdateProfileFromSession(session)
	if req.Profile != nil {
		mergeUpdateProfileRequest(updateReq, req.Profile)
	}

	profileUpdated := false
	if updateReq.hasChanges() {
		if _, err := UpdateProfile(ctx, updateReq); err != nil {
			return nil, err
		}
		profileUpdated = true
	}

	source := "ai_interview"
	published, skipped := 0, 0
	for _, item := range req.FAQ {
		if !item.Include {
			skipped++
			continue
		}
		if err := validateFAQDraft(item.Question, item.Answer); err != nil {
			skipped++
			continue
		}
		cat := item.Category
		_, err := kb.InsertKBEntryWithIndex(ctx, user.TenantSchema, user.TenantID,
			strings.TrimSpace(item.Question), strings.TrimSpace(item.Answer), cat, source, true)
		if err != nil {
			rlog.Warn("setup interview faq insert failed", "err", err)
			skipped++
			continue
		}
		published++
	}

	_ = appauth.RedisClient().Del(ctx, setupInterviewStagingKey(sessionId))

	msg := fmt.Sprintf("Profil %s. %d FAQ disimpan.", map[bool]string{true: "diperbarui", false: "tidak diubah"}[profileUpdated], published)
	if skipped > 0 {
		msg += fmt.Sprintf(" %d FAQ dilewati.", skipped)
	}

	return &SetupInterviewPublishResponse{
		ProfileUpdated: profileUpdated,
		FAQPublished:   published,
		FAQSkipped:     skipped,
		Message:        msg,
	}, nil
}

func buildUpdateProfileFromSession(session *setupInterviewSession) *UpdateProfileRequest {
	req := &UpdateProfileRequest{}
	d := session.ProfileDraft
	req.BusinessName = d.BusinessName
	req.Description = d.Description
	req.Address = d.Address
	req.OpeningHours = d.OpeningHours
	req.ProductsServices = d.ProductsServices
	req.BasePricing = d.BasePricing
	req.DeliveryArea = d.DeliveryArea
	if session.Tone != nil {
		req.Tone = session.Tone
	}
	if session.GreetingDraft != nil {
		req.GreetingTemplate = session.GreetingDraft
	}
	return req
}

func mergeUpdateProfileRequest(base *UpdateProfileRequest, over *UpdateProfileRequest) {
	if over.BusinessName != nil {
		base.BusinessName = over.BusinessName
	}
	if over.Description != nil {
		base.Description = over.Description
	}
	if over.Address != nil {
		base.Address = over.Address
	}
	if over.OpeningHours != nil {
		base.OpeningHours = over.OpeningHours
	}
	if over.ProductsServices != nil {
		base.ProductsServices = over.ProductsServices
	}
	if over.BasePricing != nil {
		base.BasePricing = over.BasePricing
	}
	if over.DeliveryArea != nil {
		base.DeliveryArea = over.DeliveryArea
	}
	if over.GreetingTemplate != nil {
		base.GreetingTemplate = over.GreetingTemplate
	}
	if over.Tone != nil {
		base.Tone = over.Tone
	}
}

func (r *UpdateProfileRequest) hasChanges() bool {
	return r.BusinessName != nil || r.Description != nil || r.Address != nil ||
		r.OpeningHours != nil || r.ProductsServices != nil || r.BasePricing != nil ||
		r.DeliveryArea != nil || r.GreetingTemplate != nil || r.Tone != nil
}
