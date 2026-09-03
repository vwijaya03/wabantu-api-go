package ai

import (
	"context"
	"fmt"

	"encore.dev/rlog"
)

// handleOrderFlow delegates checkout FSM to internal/buyerflow (satu implementasi dengan simulator).
func (s *AutoReplyService) handleOrderFlow(
	ctx context.Context,
	ts tenantScopedQuerier,
	tenantSchema, tenantID string,
	convo *dbConversation,
	channel *dbChannel,
	contact *dbContact,
	userText string,
	profile *dbBusinessProfile,
	kb []dbKBEntry,
	history []dbMessage,
	inboundID string,
) (bool, error) {
	catalog, catErr := loadActiveCatalog(ctx, ts, 40)
	if catErr != nil {
		rlog.Warn("AI order: catalog load failed", "err", catErr)
	}
	enrichCatalogStock(ctx, ts, catalog)

	vctx, catalog := s.fetchCatalogVectorContext(ctx, tenantID, tenantSchema, userText, ts, catalog)

	scopeKW := businessScopeKeywords(profile)
	formal := strOrEmpty(profile.Tone) == "formal"
	tmpl := orderTemplatesFromKB(kb, formal)

	state, _ := s.getOrderState(ctx, tenantID, convo.ID)

	send := func(text, path string) (bool, error) {
		reason := reasonNonQuestion
		author := "system"
		msg := text
		meta := metaNoLLM(reason, path)
		if path == PathCatalogDB {
			meta = metaNoLLM(reasonAIGenerated, PathCatalogDB)
			author = "ai"
			msg = applyOutputPolicy(text)
		}
		meta.LogAndRecord(ctx, convo.ID, inboundID, 0, 0)
		err := s.sendAiMessage(ctx, ts, tenantID, convo, channel, contact, msg, author, inboundID, meta)
		return err == nil, err
	}

	scope := orderAccessScope{ConversationID: convo.ID, ContactID: convo.ContactID}
	if state == nil && !IsExplicitNewOrderStart(userText) && parseOrderRefFromMessage(userText) == "" && hasPurchaseIntent(userText, catalog) {
		if drafts, derr := loadDraftOrdersForContact(ctx, ts, tenantSchema, scope); derr == nil && len(drafts) > 1 {
			return send(orderAmendPickDraftReply(drafts), PathOrderFlow)
		}
	}
	if ref := parseOrderRefFromMessage(userText); ref != "" && state == nil {
		o, denied, _ := loadOrderByRefForContact(ctx, ts, tenantSchema, scope, ref)
		if denied {
			return send(orderAccessDeniedReply(), PathOrderFlow)
		}
		if o != nil {
			formattedRef := FormatOrderNumber(o.ID)
			if isOrderAmendBlockedStatus(o.Status) {
				return send(orderAmendBlockedStatusReply(formal, o.Status, formattedRef), PathOrderFlow)
			}
			if isOrderDraftAmendable(o.Status) {
				pinned := orderState{PersistedOrderID: o.ID, Step: "ask_recipient"}
				s.setOrderState(ctx, tenantID, convo.ID, pinned)
				state = &pinned
			}
		}
	}

	if IsStructuredOrderList(userText) {
		if state != nil {
			s.clearOrderState(ctx, tenantID, convo.ID)
			state = nil
		}
		outcome := evaluateStructuredOrder(userText, catalog, formal, vctx)
		if outcome.Matched {
			if len(outcome.Lines) == 0 {
				if len(outcome.Unmatched) > 0 {
					return send(structuredOrderUnmatchedReply(formal, outcome.Unmatched), PathOrderFlow)
				}
			} else {
				if outcome.NeedVariant {
					s.setOrderState(ctx, tenantID, convo.ID, outcome.State)
					reply := catalogConfirmLine(outcome.State)
					if reply != "" {
						reply += "\n\n"
					}
					reply += tmpl.AskVariant
					return send(reply, PathOrderFlow)
				}
				if outcome.Blocked {
					return send(outcome.BlockReply, PathOrderFlow)
				}
				s.setOrderState(ctx, tenantID, convo.ID, outcome.State)
				prompt := tmpl.AskRecipient
				if len(outcome.Unmatched) > 0 {
					prompt = structuredOrderUnmatchedReply(formal, outcome.Unmatched) + "\n\n" + prompt
				}
				reply := catalogConfirmLine(outcome.State)
				if reply != "" {
					reply += "\n\n" + prompt
				} else {
					reply = prompt
				}
				if upsell := formatUpsellBlock(outcome.State, catalog); upsell != "" {
					reply += "\n\n" + upsell
				}
				return send(reply, PathOrderFlow)
			}
		}
	}

	res := AdvanceOrderFlow(OrderFlowInput{
		UserText:  userText,
		State:     state,
		Catalog:   catalog,
		History:   history,
		Profile:   profile,
		KB:        kb,
		ScopeKW:   scopeKW,
		VectorCtx: vctx,
	}, func(st orderState) (string, error) {
		if st.HasMultiItems() {
			var reply string
			var blocked bool
			st, reply, blocked = guardOrderStateQty(st, catalog, formal, "ask_qty")
			if blocked {
				return "", fmt.Errorf("%s", reply)
			}
		} else {
			reject, _, whID := ensureDraftOrderStock(ctx, ts, st)
			if reject {
				return "", fmt.Errorf("stock unavailable")
			}
			if whID != "" {
				st.WarehouseID = whID
			}
		}
		return persistDraftOrder(ctx, ts, tenantSchema, convo.ID, convo.ContactID, st)
	})

	path := res.Path
	if path == "" {
		path = PathOrderFlow
	}

	if res.Cleared {
		s.clearOrderState(ctx, tenantID, convo.ID)
	} else if res.State != nil {
		st := *res.State
		if res.State.Step == "ask_recipient" && res.State.CartReadyForDraft() {
			if orderID, needPick, pickList, err := persistDraftOrderEarly(ctx, ts, tenantSchema, convo.ID, convo.ContactID, st); err != nil {
				rlog.Warn("AI order: early draft persist failed", "err", err, "convoId", convo.ID)
			} else if needPick {
				return send(orderAmendPickDraftReply(pickList), PathOrderFlow)
			} else if orderID != "" {
				st.PersistedOrderID = orderID
			}
		}
		s.setOrderState(ctx, tenantID, convo.ID, st)
	}

	if res.Completed {
		meta := metaNoLLM(reasonNonQuestion, PathOrderFlow)
		meta.OrderID = res.OrderID
		meta.OrderAction = "create"
		meta.LogAndRecord(ctx, convo.ID, inboundID, 0, 0)
		err := s.sendAiMessage(ctx, ts, tenantID, convo, channel, contact, res.Reply, "system", inboundID, meta)
		return err == nil, err
	}

	if res.CatalogReply {
		return send(res.Reply, PathCatalogDB)
	}
	if path == PathOutOfScope {
		return send(res.Reply, PathOutOfScope)
	}
	return send(res.Reply, path)
}
