package ai

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"encore.dev/rlog"

	bf "encore.app/wabantu/internal/buyerflow"
	"encore.app/wabantu/internal/salesbrain"
)

func contactPreferredLanguage(c *dbContact) string {
	if c == nil || c.PreferredLanguage == nil {
		return ""
	}
	return strings.TrimSpace(*c.PreferredLanguage)
}

// buyerReplyLang is the language fixed system text (templates) must use for this buyer this
// turn. It is the legacy flow's ResolveBuyerLanguage (explicit request, clear message language,
// saved preference) mapped to salesbrain.Lang; an unresolved language means Indonesian, the
// tenant default. Canned text must always go through salesbrain.T with this value so an English
// buyer never receives an Indonesian-only sentence.
func buyerReplyLang(userText string, contact *dbContact) salesbrain.Lang {
	if l := salesbrain.ParseLang(ResolveBuyerLanguage(userText, contactPreferredLanguage(contact))); l != "" {
		return l
	}
	return salesbrain.LangID
}

func saveContactPreferredLanguage(ctx context.Context, ts tenantScopedQuerier, contactID, lang string) error {
	lang = strings.TrimSpace(lang)
	if contactID == "" || (lang != bf.LangEN && lang != bf.LangID) {
		return nil
	}
	_, err := ts.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s SET preferred_language = $2, updated_at = now()
		WHERE id = $1`, ts.T("contact")), contactID, lang)
	if err != nil && isMissingPIIColumn(err) {
		return nil
	}
	return err
}

// languageAskMarkers are the fragments of the language question (LanguagePreferenceAskReply and
// salesbrain.TplLanguageChoice, Indonesian and English line) that identify it in a stored message.
var languageAskMarkers = []string{"bahasa indonesia atau english", "bahasa indonesia or english"}

// lastOutboundWasLanguageAsk reports that body is the bilingual language question the bot sends
// to an unlocked buyer. Only the reply to that message is a language choice: a bare "1" after
// "How many pcs?" is a quantity.
func lastOutboundWasLanguageAsk(body string) bool {
	low := strings.ToLower(body)
	for _, m := range languageAskMarkers {
		if strings.Contains(low, m) {
			return true
		}
	}
	ask := strings.TrimSpace(bf.LanguagePreferenceAskReply())
	return ask != "" && strings.Contains(body, ask)
}

// lastAIOutboundBody is the body of the newest outbound message of the conversation ("" when none).
func lastAIOutboundBody(ctx context.Context, ts tenantScopedQuerier, convoID string) (string, error) {
	var body string
	err := ts.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT COALESCE(body, '') FROM %s
		WHERE conversation_id = $1 AND direction = 'out'
		ORDER BY created_at DESC LIMIT 1`, ts.T("message")), convoID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return body, err
}

// languageQuestionAsked reports whether the last message we sent was the language question.
// It must run before this turn sends anything (afterwards the last outbound is our own reply).
// A text that cannot be a language answer short-circuits without a query, because both consumers
// only look at the result for answer-like texts. A read error counts as "not asked": switching a
// buyer's language unprompted is the bug this guards against.
func languageQuestionAsked(ctx context.Context, ts tenantScopedQuerier, convoID, userText string) bool {
	if _, answerLike := bf.ParseLanguagePreferenceAnswer(userText); !answerLike {
		return false
	}
	body, err := lastAIOutboundBody(ctx, ts, convoID)
	if err != nil {
		rlog.Warn("AI job: could not read the last outbound message for the language check", "convoId", convoID, "err", err)
		return false
	}
	return lastOutboundWasLanguageAsk(body)
}

// languageFromBareAnswerOnly: with no language saved yet, bf.LanguageToPersist also reads a bare
// answer ("1", "2") as the buyer's choice. This reports that text is only that: not an explicit
// request and not a clear-language message, so the choice is valid only if the question was asked.
func languageFromBareAnswerOnly(userText, saved string) bool {
	if strings.TrimSpace(saved) != "" {
		return false
	}
	if _, explicit := bf.ParseExplicitLanguageSwitch(userText); explicit {
		return false
	}
	if _, clear := bf.ClearMessageLanguage(userText); clear {
		return false
	}
	_, answer := bf.ParseLanguagePreferenceAnswer(userText)
	return answer
}

// applyBuyerLanguageFromMessage runs after the turn and locks the buyer's language when the
// message decides it. languageAsked is the result of languageQuestionAsked taken before the turn.
func (s *AutoReplyService) applyBuyerLanguageFromMessage(
	ctx context.Context,
	ts tenantScopedQuerier,
	contact *dbContact,
	userText string,
	languageAsked bool,
) {
	if contact == nil {
		return
	}
	saved := contactPreferredLanguage(contact)
	lang, ok := bf.LanguageToPersist(userText, saved)
	if !ok {
		return
	}
	if !languageAsked && languageFromBareAnswerOnly(userText, saved) {
		return // a quantity or other bare number, not an answer to a question we never asked
	}
	if err := saveContactPreferredLanguage(ctx, ts, contact.ID, lang); err != nil {
		return
	}
	contact.PreferredLanguage = strPtr(lang)
	bf.SetSavedBuyerLang(lang)
}

// switchConfirmReply is the one-sentence confirmation after a language switch, in the new language.
func switchConfirmReply(lang string) string {
	return salesbrain.T(salesbrain.TplSwitchConfirm, salesbrain.ParseLang(lang))
}

// handleBuyerLanguageRouting returns handled=true when the pipeline should stop after outbound.
// languageAsked is languageQuestionAsked for this turn: only then is a bare reply such as "1" or
// "english" read as the answer to the language question. Explicit requests ("please reply in
// English") work at any time.
func (s *AutoReplyService) handleBuyerLanguageRouting(
	ctx context.Context,
	ts tenantScopedQuerier,
	payload AiReplyJobPayload,
	convo *dbConversation,
	channel *dbChannel,
	contact *dbContact,
	userText string,
	languageAsked bool,
) (handled bool, err error) {
	saved := contactPreferredLanguage(contact)

	if lang, ok := bf.ParseLanguagePreferenceAnswer(userText); ok && languageAsked {
		if err := saveContactPreferredLanguage(ctx, ts, contact.ID, lang); err != nil {
			return false, err
		}
		contact.PreferredLanguage = strPtr(lang)
		bf.SetSavedBuyerLang(lang)
		out := metaNoLLM(reasonNonQuestion, PathBuyerLanguage)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact,
			switchConfirmReply(lang), "ai", payload.InboundMessageID, out)
		return err == nil, err
	}

	if lang, ok := bf.ParseExplicitLanguageSwitch(userText); ok {
		if err := saveContactPreferredLanguage(ctx, ts, contact.ID, lang); err != nil {
			return false, err
		}
		contact.PreferredLanguage = strPtr(lang)
		bf.SetSavedBuyerLang(lang)
		if !bf.ExplicitLanguageSwitchHasShoppingRequest(userText) {
			out := metaNoLLM(reasonNonQuestion, PathBuyerLanguage)
			out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
			err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact,
				switchConfirmReply(lang), "ai", payload.InboundMessageID, out)
			return err == nil, err
		}
		return false, nil
	}

	if bf.ShouldAskBuyerLanguagePreference(userText, saved) {
		out := metaNoLLM(reasonNonQuestion, PathBuyerLanguage)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err = s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact,
			bf.LanguagePreferenceAskReply(), "ai", payload.InboundMessageID, out)
		return err == nil, err
	}
	return false, nil
}

func loadContactPreferredLanguage(ctx context.Context, ts tenantScopedQuerier, id string) (*string, error) {
	row := ts.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT preferred_language FROM %s WHERE id = $1`, ts.T("contact")), id)
	var pref sql.NullString
	err := row.Scan(&pref)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		if isMissingPIIColumn(err) {
			return nil, nil
		}
		return nil, err
	}
	if !pref.Valid || strings.TrimSpace(pref.String) == "" {
		return nil, nil
	}
	s := strings.TrimSpace(pref.String)
	return &s, nil
}
