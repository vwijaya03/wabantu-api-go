package ai

import (
	"context"
	"fmt"
	"strings"
	"time"

	"encore.dev/rlog"

	"encore.app/wabantu/internal/salesbrain"
	"encore.app/wabantu/notification"
	"encore.app/wabantu/tenantaccess"
)

// Handoff v2: the AI never goes silent without a way back and the owner always hears about it.
//
//   - Hard handoff (the buyer asked for a person): honest takeover text, pause with
//     HandoffReasonHumanRequest, owner notification. The AI resumes by itself after AutoResumeAfter
//     if no staff member replied (shouldAutoResume).
//   - Soft handoff (no grounded answer): tell the buyer, notify the owner, keep serving. No pause.
//   - System failure (LLM error, retries exhausted): honest "busy" text in the buyer's language and
//     an owner notification. No pause.
//
// Staff takeovers from the inbox never auto-resume: only pauses the AI created itself do.
const (
	HandoffReasonHumanRequest = "human_request" // buyer explicitly asked for a person
	HandoffReasonAIFallback   = "ai_fallback"   // legacy name kept for FallbackAutoReply rows already in DB
	AutoResumeAfter           = 2 * time.Hour
)

// Reasons the AI wrote into conversation.handoff_reason before handoff v2. Rows with these values
// are still in the database, and they are the AI's own pauses, so they resume like the new ones.
const (
	handoffReasonBuyerRequestedHuman = "buyer_requested_human"                // handleBuyerHumanHandoff, before human_request
	handoffReasonAutoFallback        = "Auto fallback setelah retry AI gagal" // FallbackAutoReply, before it stopped pausing
	// handoffReasonSensitiveEscalate is still written: a sensitive message pauses the AI (hard
	// handoff) and, like every AI-initiated pause, resumes after AutoResumeAfter without a staff reply.
	handoffReasonSensitiveEscalate = "Sensitive/escalate detected"
)

// Owner notification kinds (app_notification.kind).
const (
	notifyKindHandoff     = "ai.handoff"
	notifyKindSystemError = "ai.system_error"
)

// Owner-facing copy (the dashboard notification list is Indonesian, like the other kinds).
const (
	notifyTitleHumanRequest = "Pembeli minta dibantu tim"
	notifyTitleNoAnswer     = "Pembeli butuh jawaban dari tim"
	notifyTitleSystemError  = "Balasan AI gagal"

	notifyReasonHumanRequest = "pembeli meminta staf; AI dijeda dan lanjut otomatis bila belum dibalas dalam 2 jam."
	notifyReasonNoAnswer     = "AI belum punya jawaban dari katalog/FAQ; AI tetap melayani."
	notifyReasonLLMError     = "LLM gagal menjawab; pembeli sudah diberi tahu (cek AI Provider dan kuota)."
	notifyReasonFallback     = "AI gagal membalas setelah beberapa percobaan; pembeli sudah diberi tahu."
)

const (
	// ownerInboxLink is the dashboard inbox. The inbox keeps the selected conversation in client
	// state only (no query parameter to deep-link), so the plain route is the best link.
	ownerInboxLink = "/dashboard/inbox"

	notifyDedupeTTL = time.Hour
)

// Seams for the two cross-service helpers (swapped in tests only).
var (
	listOwnerAccountIDsFn     = tenantaccess.ListOwnerAccountIDs
	createOwnerNotificationFn = notification.CreateForAccounts
	autoResumeNow             = time.Now
)

// isAIInitiatedPause reports whether reason was written by the AI itself. Only those pauses may
// auto-resume. Staff takeover from the inbox must never auto-resume: it writes either the backend
// default "Diambil alih manual oleh staff", the dashboard's "Takeover manual dari inbox", or free
// text, so this is a whitelist and everything else (including "" and "workflow") is false.
func isAIInitiatedPause(reason string) bool {
	switch strings.TrimSpace(reason) {
	case AiNoAnswerPauseReason, HandoffReasonHumanRequest, HandoffReasonAIFallback,
		handoffReasonBuyerRequestedHuman, handoffReasonAutoFallback, handoffReasonSensitiveEscalate:
		return true
	}
	return false
}

// shouldAutoResume: AI-initiated pause, paused at least AutoResumeAfter ago, no staff reply since.
func shouldAutoResume(reason string, pausedAt *time.Time, now time.Time, staffRepliedSince bool) bool {
	if pausedAt == nil || staffRepliedSince || !isAIInitiatedPause(reason) {
		return false
	}
	return now.Sub(*pausedAt) >= AutoResumeAfter
}

// staffInvolvedSince reports whether a human took part after the pause: a staff reply since
// `since`, or the conversation being assigned to a staff member (the PATCH takeover keeps the
// previous handoff_reason, so the reason alone cannot tell staff took over).
func staffInvolvedSince(ctx context.Context, ts tenantScopedQuerier, convoID string, since time.Time) (bool, error) {
	var replied, assigned bool
	err := ts.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT EXISTS (SELECT 1 FROM %[1]s
		               WHERE conversation_id = $1 AND direction = 'out' AND author = 'human' AND created_at > $2),
		       EXISTS (SELECT 1 FROM %[2]s
		               WHERE id = $1 AND (assigned_to_user_id IS NOT NULL OR COALESCE(assigned_to_name, '') <> ''))`,
		ts.T("message"), ts.T("conversation")), convoID, since).Scan(&replied, &assigned)
	return replied || assigned, err
}

// pausedConversationMayResume decides whether a paused conversation goes back to the AI. It is
// the cheap, DB-free check first; the staff query only runs for an old AI-initiated pause. An
// error keeps the conversation paused.
func pausedConversationMayResume(ctx context.Context, ts tenantScopedQuerier, convo *dbConversation) (bool, error) {
	reason := ""
	if convo.HandoffReason != nil {
		reason = *convo.HandoffReason
	}
	now := autoResumeNow()
	if !shouldAutoResume(reason, convo.AIPausedAt, now, false) {
		return false, nil
	}
	involved, err := staffInvolvedSince(ctx, ts, convo.ID, *convo.AIPausedAt)
	if err != nil {
		return false, err
	}
	return shouldAutoResume(reason, convo.AIPausedAt, now, involved), nil
}

// resumeAIAfterPause hands the conversation back to the AI and apologises once for the wait. The
// UPDATE only matches a conversation that is still paused, so when two jobs race only one apologises.
func (s *AutoReplyService) resumeAIAfterPause(
	ctx context.Context,
	ts tenantScopedQuerier,
	payload AiReplyJobPayload,
	convo *dbConversation,
	channel *dbChannel,
	contact *dbContact,
	userText string,
) error {
	res, err := ts.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s
		SET ai_handled = true, ai_paused_at = NULL, handoff_reason = NULL, updated_at = now()
		WHERE id = $1 AND ai_handled = false`, ts.T("conversation")), convo.ID)
	if err != nil {
		return err
	}
	convo.AIHandled = true
	convo.AIPausedAt = nil
	convo.HandoffReason = nil
	if n, _ := res.RowsAffected(); n == 0 {
		return nil // another job resumed it first and already apologised
	}
	rlog.Info("AI job: conversation auto-resumed after staff silence", "convoId", convo.ID, "inboundId", payload.InboundMessageID)

	lang := buyerReplyLang(userText, contact)
	out := metaNoLLM(reasonNonQuestion, PathHumanHandoff)
	out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
	// Empty inbound id on purpose: sendAiMessage is idempotent per inbound message, and the real
	// reply to this inbound still has to go out right after this notice.
	if err := s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact,
		salesbrain.T(salesbrain.TplHandoffResumed, lang), "ai", "", out); err != nil {
		rlog.Warn("AI job: resume notice not sent", "convoId", convo.ID, "err", err)
	}
	return nil
}

// handleAiSystemError answers a technical failure (LLM error, retries exhausted): an honest
// "busy" message in the buyer's language and an owner notification. It never pauses the AI. meta
// is the caller's existing metadata path so dashboards keep counting these replies.
func (s *AutoReplyService) handleAiSystemError(
	ctx context.Context,
	ts tenantScopedQuerier,
	payload AiReplyJobPayload,
	convo *dbConversation,
	channel *dbChannel,
	contact *dbContact,
	profile *dbBusinessProfile,
	userText string,
	meta AiReplyMeta,
	reason string,
) (bool, error) {
	lang := buyerReplyLang(userText, contact)
	meta.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
	text := salesbrain.T(salesbrain.TplSystemBusy, lang, shopLabel(profile, lang))
	if err := s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, applyOutputPolicy(text),
		"system", payload.InboundMessageID, meta); err != nil {
		return false, err
	}
	s.notifyOwner(ctx, payload.TenantID, convo.ID, notifyKindSystemError, notifyTitleSystemError,
		ownerNotificationSummary(userText, reason))
	return true, nil
}

// shopLabel is the shop name for templates; an unnamed shop reads as "toko" / "shop".
func shopLabel(profile *dbBusinessProfile, lang salesbrain.Lang) string {
	if profile != nil {
		if n := strings.TrimSpace(profile.BusinessName); n != "" {
			return n
		}
	}
	if lang == salesbrain.LangEN {
		return "shop"
	}
	return "toko"
}

// ownerNotificationSummary is the two-line body of an owner notification: the buyer's message
// (preview, PII-redacted outside local) and why the AI needs help.
func ownerNotificationSummary(userText, reason string) string {
	preview := previewText(strings.Join(strings.Fields(userText), " "), 160)
	return fmt.Sprintf("Pesan pembeli: \"%s\"\nAlasan: %s", preview, reason)
}

func notifyDedupeKey(kind, tenantID, convoID string) string {
	return fmt.Sprintf("ai:notify:%s:%s:%s", kind, tenantID, convoID)
}

// notifyOwner sends one in-app notification to the tenant owners, deduped per kind+conversation
// for one hour via Redis. Errors are logged, never returned to the reply path.
func (s *AutoReplyService) notifyOwner(ctx context.Context, tenantID, convoID, kind, title, body string) {
	if tenantID == "" || convoID == "" {
		return
	}
	key := ""
	if s != nil && s.rdb != nil {
		key = notifyDedupeKey(kind, tenantID, convoID)
		claimed, err := s.rdb.SetNX(ctx, key, "1", notifyDedupeTTL).Result()
		switch {
		case err != nil:
			// Redis trouble must not swallow the notification: send without dedupe.
			rlog.Warn("AI notify owner: dedupe unavailable, sending anyway", "kind", kind, "err", err)
			key = ""
		case !claimed:
			rlog.Info("AI notify owner: duplicate within the hour, skipped", "kind", kind, "convoId", convoID)
			return
		}
	}
	release := func() { // a failed send must not hold the dedupe slot
		if key != "" {
			s.rdb.Del(ctx, key)
		}
	}
	ownerIDs, err := listOwnerAccountIDsFn(ctx, tenantID)
	if err != nil {
		rlog.Warn("AI notify owner: owner lookup failed", "kind", kind, "tenantId", tenantID, "err", err)
		release()
		return
	}
	if len(ownerIDs) == 0 {
		rlog.Warn("AI notify owner: tenant has no owner account", "kind", kind, "tenantId", tenantID)
		return
	}
	if err := createOwnerNotificationFn(ctx, ownerIDs, kind, title, body, ownerInboxLink); err != nil {
		rlog.Warn("AI notify owner: create failed", "kind", kind, "tenantId", tenantID, "err", err)
		release()
	}
}
