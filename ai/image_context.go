package ai

import (
	"context"
	"fmt"
	"strings"
	"time"

	"encore.dev/rlog"

	"encore.app/wabantu/aivision"
	"encore.app/wabantu/inbox"
	"encore.app/wabantu/shared/inboxrealtime"
	"encore.app/wabantu/usage"
)

const (
	imageContextVisionRatePerHour = 5
	imageContextVisionRatePrefix  = "image_context:vision:"
	productImageMatchMinConf      = 0.85

	imageContextFallbackMsg = "Maaf kak, untuk gambar tanpa keterangan saya belum bisa bantu. Bisa ketik pertanyaannya, atau ketik *bantuan* untuk dihubungkan ke tim."
)

func imageContextJobFromPaymentProof(job *PaymentProofJob) *ImageContextJob {
	if job == nil {
		return nil
	}
	return &ImageContextJob{
		TenantSchema:     job.TenantSchema,
		TenantID:         job.TenantID,
		ConversationID:   job.ConversationID,
		ContactID:        job.ContactID,
		MessageID:        job.MessageID,
		InboundMessageID: job.InboundMessageID,
	}
}

func processImageContextJob(ctx context.Context, job *ImageContextJob) error {
	ts, err := openTenantScope(ctx, job.TenantSchema)
	if err != nil {
		return err
	}

	scope := orderAccessScope{
		ConversationID: job.ConversationID,
		ContactID:      job.ContactID,
	}
	if !scope.valid() {
		return nil
	}

	msg, err := loadMessage(ctx, ts, job.MessageID)
	if err != nil {
		return err
	}
	if !shouldProcessImageContext(msg) {
		rlog.Info("image context skipped: has caption or not image", "messageId", job.MessageID)
		return nil
	}

	convo, err := loadConversation(ctx, ts, job.ConversationID)
	if err != nil {
		return err
	}
	if convo == nil || !convo.AIHandled {
		rlog.Info("image context skipped: conversation handoff", "conversationId", job.ConversationID)
		return nil
	}

	profile, err := loadBusinessProfile(ctx, ts)
	if err != nil {
		return err
	}

	if profile != nil && profile.AIEnabled {
		if handled, herr := tryProductImageMatch(ctx, ts, job, profile); herr != nil {
			return herr
		} else if handled {
			return nil
		}
	}

	return sendImageContextFallback(ctx, ts, job)
}

// shouldProcessImageContext — Fase 3c/3d hanya untuk gambar tanpa caption (autoreply 3a menangani caption).
func shouldProcessImageContext(msg *dbMessage) bool {
	if msg == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(msg.Type), "image") {
		return false
	}
	return msg.Body == ""
}

func tryProductImageMatch(ctx context.Context, ts tenantScopedQuerier, job *ImageContextJob, profile *dbBusinessProfile) (bool, error) {
	if !checkImageContextVisionRateLimit(ctx, job.TenantSchema, job.ContactID) {
		rlog.Info("image context vision rate limited", "contactId", job.ContactID)
		return false, nil
	}

	catalog, err := loadActiveCatalog(ctx, ts, defaultCatalogLoadLimit)
	if err != nil {
		return false, err
	}
	if len(catalog) == 0 {
		return false, nil
	}

	allowed, _, _ := usage.CheckQuota(ctx, job.TenantSchema, "ai_token")
	if !allowed {
		rlog.Info("image context vision skipped: quota exceeded", "schema", job.TenantSchema)
		return false, nil
	}

	imageBytes, mime, err := inbox.FetchMessageMediaBytes(ctx, job.TenantSchema, job.MessageID)
	if err != nil {
		return false, err
	}

	extract, visUsage, visErr := aivision.ExtractProductMatchFromImage(ctx, secrets.AnthropicAPIKey, imageBytes, mime)
	_ = usage.RecordAIActivity(ctx, usage.AIActivityParams{
		TenantSchema:   job.TenantSchema,
		TenantID:       job.TenantID,
		ConversationID: job.ConversationID,
		InboundID:      job.InboundMessageID,
		Purpose:        usage.PurposeProductImageMatch,
		Path:           PathProductImageMatch,
		Model:          "claude-haiku-4-5",
		LLMUsed:        visErr == nil,
		InputTokens:    visUsage.InputTokens,
		OutputTokens:   visUsage.OutputTokens,
	})
	if visErr != nil {
		rlog.Warn("product image vision failed", "messageId", job.MessageID, "err", visErr)
		return false, nil
	}

	match := matchCatalogFromVision(extract, catalog)
	if match == nil {
		rlog.Info("image context: no catalog match", "messageId", job.MessageID, "confidence", extract.Confidence)
		return false, nil
	}

	enrichCatalogStock(ctx, ts, []dbCatalogItem{*match})
	formal := profile != nil && strOrEmpty(profile.Tone) == "formal"
	reply := buildCatalogItemReply(formal, match, 0)
	if strings.TrimSpace(reply) == "" {
		return false, nil
	}

	meta := metaNoLLM(reasonAIGenerated, PathProductImageMatch)
	if err := sendImageContextOutbound(ctx, ts, job, reply, meta); err != nil {
		return false, err
	}

	if job.TenantID != "" && svc != nil && svc.rdb != nil {
		inboxrealtime.Publish(ctx, svc.rdb, job.TenantID)
	}
	return true, nil
}

func matchCatalogFromVision(extract aivision.ProductImageMatchExtract, catalog []dbCatalogItem) *dbCatalogItem {
	if len(catalog) == 0 || extract.Confidence < productImageMatchMinConf {
		return nil
	}
	hint := strings.TrimSpace(extract.SkuHint)
	if hint != "" {
		hintLower := strings.ToLower(hint)
		for i := range catalog {
			it := &catalog[i]
			code := strings.ToLower(strings.TrimSpace(it.ExternalCode))
			if code == "" {
				continue
			}
			if strings.EqualFold(it.ExternalCode, hint) ||
				strings.Contains(code, hintLower) ||
				strings.Contains(hintLower, code) {
				return it
			}
		}
	}
	if name := strings.TrimSpace(extract.ProductName); name != "" {
		if m := matchCatalogItem(name, catalog); m != nil {
			return m
		}
	}
	if desc := strings.TrimSpace(extract.VisualDescription); desc != "" {
		return matchCatalogItem(desc, catalog)
	}
	return nil
}

func sendImageContextFallback(ctx context.Context, ts tenantScopedQuerier, job *ImageContextJob) error {
	meta := metaNoLLM(reasonAIGenerated, PathImageFallback)
	_ = usage.RecordAIActivity(ctx, usage.AIActivityParams{
		TenantSchema:   job.TenantSchema,
		TenantID:       job.TenantID,
		ConversationID: job.ConversationID,
		InboundID:      job.InboundMessageID,
		Purpose:        usage.PurposeInboundAutoreply,
		Path:           PathImageFallback,
		LLMUsed:        false,
	})
	if err := sendImageContextOutbound(ctx, ts, job, imageContextFallbackMsg, meta); err != nil {
		return err
	}
	if job.TenantID != "" && svc != nil && svc.rdb != nil {
		inboxrealtime.Publish(ctx, svc.rdb, job.TenantID)
	}
	return nil
}

func sendImageContextOutbound(ctx context.Context, ts tenantScopedQuerier, job *ImageContextJob, text string, meta AiReplyMeta) error {
	if svc == nil {
		return fmt.Errorf("ai service not initialized")
	}
	convo, err := loadConversation(ctx, ts, job.ConversationID)
	if err != nil {
		return err
	}
	if convo == nil {
		return fmt.Errorf("conversation not found")
	}
	contact, err := loadContact(ctx, ts, convo.ContactID)
	if err != nil {
		return err
	}
	channel, err := loadChannel(ctx, ts, convo.ChannelID)
	if err != nil {
		return err
	}
	return svc.sendAiMessage(ctx, ts, job.TenantID, convo, channel, contact, text, "ai", job.InboundMessageID, meta)
}

func checkImageContextVisionRateLimit(ctx context.Context, tenantSchema, contactID string) bool {
	if svc == nil || svc.rdb == nil {
		return true
	}
	key := imageContextVisionRatePrefix + tenantSchema + ":" + contactID
	n, err := svc.rdb.Incr(ctx, key).Result()
	if err != nil {
		return true
	}
	if n == 1 {
		_ = svc.rdb.Expire(ctx, key, time.Hour).Err()
	}
	return n <= imageContextVisionRatePerHour
}
