package ai

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	bf "encore.app/wabantu/internal/buyerflow"
)

func contactPreferredLanguage(c *dbContact) string {
	if c == nil || c.PreferredLanguage == nil {
		return ""
	}
	return strings.TrimSpace(*c.PreferredLanguage)
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

func (s *AutoReplyService) applyBuyerLanguageFromMessage(
	ctx context.Context,
	ts tenantScopedQuerier,
	contact *dbContact,
	userText string,
) {
	if contact == nil {
		return
	}
	saved := contactPreferredLanguage(contact)
	lang, ok := bf.LanguageToPersist(userText, saved)
	if !ok {
		return
	}
	if err := saveContactPreferredLanguage(ctx, ts, contact.ID, lang); err != nil {
		return
	}
	contact.PreferredLanguage = strPtr(lang)
	bf.SetSavedBuyerLang(lang)
}

// handleBuyerLanguageRouting returns handled=true when the pipeline should stop after outbound.
func (s *AutoReplyService) handleBuyerLanguageRouting(
	ctx context.Context,
	ts tenantScopedQuerier,
	payload AiReplyJobPayload,
	convo *dbConversation,
	channel *dbChannel,
	contact *dbContact,
	userText string,
) (handled bool, err error) {
	saved := contactPreferredLanguage(contact)

	if lang, ok := bf.ParseLanguagePreferenceAnswer(userText); ok && saved == "" {
		if err := saveContactPreferredLanguage(ctx, ts, contact.ID, lang); err != nil {
			return false, err
		}
		contact.PreferredLanguage = strPtr(lang)
		bf.SetSavedBuyerLang(lang)
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
				bf.LanguageSwitchConfirmReply(lang), "ai", payload.InboundMessageID, out)
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
