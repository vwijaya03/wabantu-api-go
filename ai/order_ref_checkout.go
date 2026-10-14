package ai

import (
	"context"

	bf "encore.app/wabantu/internal/buyerflow"
)

func profileToBF(p *dbBusinessProfile) *bf.BusinessProfile {
	if p == nil {
		return nil
	}
	return &bf.BusinessProfile{
		BusinessName:     p.BusinessName,
		Description:      p.Description,
		Address:          p.Address,
		OpeningHours:     p.OpeningHours,
		ProductsServices: p.ProductsServices,
		BasePricing:      p.BasePricing,
		DeliveryArea:     p.DeliveryArea,
		GreetingTemplate: p.GreetingTemplate,
		Tone:             p.Tone,
		CatalogURL:       p.CatalogURL,
	}
}

func kbToBF(kb []dbKBEntry) []bf.KBEntry {
	if len(kb) == 0 {
		return nil
	}
	out := make([]bf.KBEntry, len(kb))
	copy(out, kb)
	return out
}

func (s *AutoReplyService) handleOrderRefCheckoutContinuation(
	ctx context.Context,
	ts tenantScopedQuerier,
	payload AiReplyJobPayload,
	convo *dbConversation,
	channel *dbChannel,
	contact *dbContact,
	userText string,
	profile *dbBusinessProfile,
	kbEntries []dbKBEntry,
	history []dbMessage,
	catalog []dbCatalogItem,
) (bool, error) {
	if !bf.IsOrderRefCheckoutContinuation(userText) {
		return false, nil
	}
	scope := orderAccessScope{ConversationID: convo.ID, ContactID: convo.ContactID}
	formal := strOrEmpty(profile.Tone) == "formal"
	send := func(text, path string) (bool, error) {
		out := metaNoLLM(reasonAIGenerated, path)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err := s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, applyOutputPolicy(text), "ai", payload.InboundMessageID, out)
		return err == nil, err
	}

	refs := bf.ParseAllOrderRefsFromMessage(userText)
	if len(refs) > 1 {
		return send(bf.OrderRefCheckoutPickOneReply(refs, formal), PathOrderFlow)
	}
	ref := refs[0]
	o, denied, err := loadOrderByRefForContact(ctx, ts, payload.TenantSchema, scope, ref)
	if err != nil {
		return false, err
	}
	if denied {
		return send(orderAccessDeniedReply(), PathOrderFlow)
	}
	if o == nil {
		return send(orderRefNotFoundReply(ref), PathOrderFlow)
	}
	formattedRef := FormatOrderNumber(o.ID)
	if isOrderAmendBlockedStatus(o.Status) {
		return send(orderAmendBlockedStatusReply(formal, o.Status, formattedRef), PathOrderFlow)
	}
	if !isOrderDraftAmendable(o.Status) {
		return send(bf.OrderRefCheckoutNonDraftReply(formal, o.Status, formattedRef), PathOrderFlow)
	}

	pinned := orderStateFromPersistedDraft(o)
	s.setOrderState(ctx, payload.TenantID, convo.ID, pinned)
	return s.handleOrderFlow(ctx, ts, payload.TenantSchema, payload.TenantID, convo, channel, contact,
		userText, profile, kbEntries, history, payload.InboundMessageID)
}
