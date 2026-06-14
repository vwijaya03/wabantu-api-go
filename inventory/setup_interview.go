package inventory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appauth "encore.app/wabantu/auth"
	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/tenant"
	"encore.app/wabantu/usage"
)

const (
	invSetupInterviewTTL     = 24 * time.Hour
	invSetupInterviewMaxTurn = 12
	invSetupInterviewMaxMsg  = 2000
)

type invSetupMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type invSetupInterviewSession struct {
	SessionID              string            `json:"sessionId"`
	TenantSchema           string            `json:"tenantSchema"`
	UploadedBy             string            `json:"uploadedBy"`
	Phase                  string            `json:"phase"`
	Messages               []invSetupMessage `json:"messages"`
	AnswersDraft           WizardAnswers     `json:"answersDraft"`
	ReadyForRecommendation bool              `json:"readyForRecommendation"`
	TurnCount              int               `json:"turnCount"`
	CreatedAt              time.Time         `json:"createdAt"`
}

type InvSetupInterviewStartResponse struct {
	SessionID              string            `json:"sessionId"`
	Phase                  string            `json:"phase"`
	Messages               []invSetupMessage `json:"messages"`
	AnswersDraft           WizardAnswers     `json:"answersDraft"`
	ReadyForRecommendation bool              `json:"readyForRecommendation"`
	TokenQuotaRemaining    int               `json:"tokenQuotaRemaining"`
	TokenQuotaLimit        int               `json:"tokenQuotaLimit"`
	QuotaNotice            string            `json:"quotaNotice"`
}

type InvSetupInterviewMessageRequest struct {
	Message string `json:"message"`
}

type InvSetupInterviewMessageResponse struct {
	SessionID              string            `json:"sessionId"`
	Phase                  string            `json:"phase"`
	Messages               []invSetupMessage `json:"messages"`
	AnswersDraft           WizardAnswers     `json:"answersDraft"`
	ReadyForRecommendation bool              `json:"readyForRecommendation"`
	TokenQuotaRemaining    int               `json:"tokenQuotaRemaining"`
	TokenQuotaLimit        int               `json:"tokenQuotaLimit"`
	QuotaNotice            string            `json:"quotaNotice"`
	TokensUsed             int               `json:"tokensUsed"`
}

type InvSetupInterviewGetResponse struct {
	SessionID              string            `json:"sessionId"`
	Phase                  string            `json:"phase"`
	Messages               []invSetupMessage `json:"messages"`
	AnswersDraft           WizardAnswers     `json:"answersDraft"`
	ReadyForRecommendation bool              `json:"readyForRecommendation"`
	TokenQuotaRemaining    int               `json:"tokenQuotaRemaining"`
	TokenQuotaLimit        int               `json:"tokenQuotaLimit"`
	QuotaNotice            string            `json:"quotaNotice"`
}

func invSetupInterviewKey(sessionID string) string {
	return "inventory:setup-interview:" + sessionID
}

func invSetupQuotaNotice(rem, lim int) string {
	if lim <= 0 {
		return "Setiap balasan wawancara setup memakai kuota token AI bulanan toko Anda."
	}
	return fmt.Sprintf("Setiap balasan memakai kuota token AI. Sisa: %d dari %d.", rem, lim)
}

func saveInvSetupSession(ctx context.Context, s *invSetupInterviewSession) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return appauth.RedisClient().Set(ctx, invSetupInterviewKey(s.SessionID), raw, invSetupInterviewTTL).Err()
}

func loadInvSetupSession(ctx context.Context, sessionID, tenantSchema string) (*invSetupInterviewSession, error) {
	raw, err := appauth.RedisClient().Get(ctx, invSetupInterviewKey(sessionID)).Bytes()
	if err != nil {
		return nil, appErrs.NotFound("sesi wawancara tidak ditemukan atau sudah kedaluwarsa")
	}
	var s invSetupInterviewSession
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, appErrs.Internal("sesi wawancara rusak")
	}
	if s.TenantSchema != tenantSchema {
		return nil, appErrs.Forbidden("sesi wawancara tidak valid")
	}
	return &s, nil
}

func sessionToInvSetupGetResponse(s *invSetupInterviewSession, rem, lim int) *InvSetupInterviewGetResponse {
	base := sessionToInvSetupResponse(s, rem, lim)
	return &InvSetupInterviewGetResponse{
		SessionID:              base.SessionID,
		Phase:                  base.Phase,
		Messages:               base.Messages,
		AnswersDraft:           base.AnswersDraft,
		ReadyForRecommendation: base.ReadyForRecommendation,
		TokenQuotaRemaining:    base.TokenQuotaRemaining,
		TokenQuotaLimit:        base.TokenQuotaLimit,
		QuotaNotice:            base.QuotaNotice,
	}
}

func sessionToInvSetupResponse(s *invSetupInterviewSession, rem, lim int) *InvSetupInterviewStartResponse {
	msgs := s.Messages
	if msgs == nil {
		msgs = []invSetupMessage{}
	}
	return &InvSetupInterviewStartResponse{
		SessionID:              s.SessionID,
		Phase:                  s.Phase,
		Messages:               msgs,
		AnswersDraft:           s.AnswersDraft,
		ReadyForRecommendation: s.ReadyForRecommendation,
		TokenQuotaRemaining:    rem,
		TokenQuotaLimit:        lim,
		QuotaNotice:            invSetupQuotaNotice(rem, lim),
	}
}

func initialInvSetupMessage(biz businessWizardContext) string {
	greet := "Halo! Saya bantu menyiapkan metode HPP (cara hitung biaya stok) yang cocok untuk toko Anda."
	if biz.BusinessName != "" {
		greet = fmt.Sprintf("Halo! Saya bantu menyiapkan metode HPP untuk %s.", biz.BusinessName)
	}
	return greet + " Ceritakan singkat: Anda jual apa, dan bagaimana pola stoknya? (misalnya frozen food, fashion, atau retail umum — stok cepat/lambat, harga beli naik-turun, dll.)"
}

func wizardAnswersReady(a WizardAnswers) bool {
	return strings.TrimSpace(a.BusinessType) != "" && len(strings.TrimSpace(a.ProductDescription)) >= 20
}

//encore:api auth method=POST path=/api/v1/inventory/setup-interview/start tag:owner
func StartInvSetupInterview(ctx context.Context) (*InvSetupInterviewStartResponse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}

	_, rem, lim := usage.CheckQuota(ctx, u.TenantSchema, "ai_token")

	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	if _, err := loadSetting(ctx, conn); err != nil {
		return nil, err
	}
	biz := loadBusinessWizardContext(ctx, conn)

	sessionID := fmt.Sprintf("inv_setup_%d", time.Now().UnixNano())
	session := &invSetupInterviewSession{
		SessionID:    sessionID,
		TenantSchema: u.TenantSchema,
		UploadedBy:   u.AccountID,
		Phase:        "intro",
		Messages: []invSetupMessage{
			{Role: "assistant", Content: initialInvSetupMessage(biz)},
		},
		AnswersDraft: WizardAnswers{},
		CreatedAt:    time.Now(),
	}
	if err := saveInvSetupSession(ctx, session); err != nil {
		return nil, appErrs.Internal("gagal membuat sesi wawancara")
	}
	return sessionToInvSetupResponse(session, rem, lim), nil
}

//encore:api auth method=GET path=/api/v1/inventory/setup-interview/session/:sessionId tag:owner
func GetInvSetupInterviewSession(ctx context.Context, sessionId string) (*InvSetupInterviewGetResponse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}
	session, err := loadInvSetupSession(ctx, sessionId, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	_, rem, lim := usage.CheckQuota(ctx, u.TenantSchema, "ai_token")
	return sessionToInvSetupGetResponse(session, rem, lim), nil
}

//encore:api auth method=POST path=/api/v1/inventory/setup-interview/session/:sessionId/message tag:owner
func SendInvSetupInterviewMessage(ctx context.Context, sessionId string, req *InvSetupInterviewMessageRequest) (*InvSetupInterviewMessageResponse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, appErrs.BadRequest("message wajib diisi")
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		return nil, appErrs.BadRequest("message wajib diisi")
	}
	if len(msg) > invSetupInterviewMaxMsg {
		return nil, appErrs.BadRequest("pesan terlalu panjang")
	}

	ok, _, lim := usage.CheckQuota(ctx, u.TenantSchema, "ai_token")
	if !ok && lim > 0 {
		return nil, appErrs.BadRequest("kuota token AI bulan ini habis")
	}

	session, err := loadInvSetupSession(ctx, sessionId, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	if session.TurnCount >= invSetupInterviewMaxTurn {
		return nil, appErrs.BadRequest("batas percakapan tercapai — lanjut ke rekomendasi HPP")
	}
	if session.ReadyForRecommendation {
		return nil, appErrs.BadRequest("wawancara sudah cukup — lanjut ke rekomendasi HPP")
	}

	session.Messages = append(session.Messages, invSetupMessage{Role: "user", Content: msg})
	session.TurnCount++

	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	biz := loadBusinessWizardContext(ctx, conn)

	turn, tokens, terr := completeInvSetupInterviewTurn(ctx, u, session, biz, msg)
	if terr != nil {
		return nil, appErrs.Internal("AI wawancara gagal: " + terr.Error())
	}

	mergeWizardAnswersUpdate(&session.AnswersDraft, turn.AnswersUpdate)
	session.Phase = normalizeInvSetupPhase(turn.Phase)
	if wizardAnswersReady(session.AnswersDraft) {
		session.Phase = "ready"
	}
	session.ReadyForRecommendation = turn.ReadyForRecommendation || session.Phase == "ready"
	if strings.Contains(strings.ToLower(msg), "cukup") || strings.Contains(strings.ToLower(msg), "lanjut") {
		if wizardAnswersReady(session.AnswersDraft) {
			session.ReadyForRecommendation = true
			session.Phase = "ready"
		}
	}

	session.Messages = append(session.Messages, invSetupMessage{
		Role:    "assistant",
		Content: turn.AssistantMessage,
	})

	if err := saveInvSetupSession(ctx, session); err != nil {
		return nil, appErrs.Internal("gagal menyimpan sesi")
	}

	_, remAfter, _ := usage.CheckQuota(ctx, u.TenantSchema, "ai_token")
	base := sessionToInvSetupResponse(session, remAfter, lim)
	return &InvSetupInterviewMessageResponse{
		SessionID:              base.SessionID,
		Phase:                  base.Phase,
		Messages:               base.Messages,
		AnswersDraft:           base.AnswersDraft,
		ReadyForRecommendation: base.ReadyForRecommendation,
		TokenQuotaRemaining:    base.TokenQuotaRemaining,
		TokenQuotaLimit:        base.TokenQuotaLimit,
		QuotaNotice:            base.QuotaNotice,
		TokensUsed:             tokens,
	}, nil
}

//encore:api auth method=POST path=/api/v1/inventory/setup-interview/session/:sessionId/finish tag:owner
func FinishInvSetupInterview(ctx context.Context, sessionId string) (*WizardRecommendResponse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}

	session, err := loadInvSetupSession(ctx, sessionId, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	answers := session.AnswersDraft
	sanitizeWizardAnswers(&answers)
	if !wizardAnswersReady(answers) {
		return nil, appErrs.BadRequest("ceritakan produk & pola stok minimal 20 karakter sebelum rekomendasi")
	}

	resp, err := runWizardRecommend(ctx, u, answers)
	if err != nil {
		return nil, err
	}
	_ = appauth.RedisClient().Del(ctx, invSetupInterviewKey(sessionId))
	return resp, nil
}
