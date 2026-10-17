package ai

import (
	"context"
	"fmt"
	"strings"

	"encore.dev/rlog"

	bf "encore.app/wabantu/internal/buyerflow"
	"encore.app/wabantu/internal/salesbrain"
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
	catalog, catErr := loadActiveCatalog(ctx, ts, defaultCatalogLoadLimit)
	if catErr != nil {
		rlog.Warn("AI order: catalog load failed", "err", catErr)
	}
	enrichCatalogStock(ctx, ts, catalog)
	if contact != nil {
		applyContactTierPrices(ctx, ts, catalog, contact.ID)
	}

	vctx, catalog := s.fetchCatalogVectorContext(ctx, tenantID, tenantSchema, userText, ts, catalog)

	scopeKW := businessScopeKeywords(profile)
	formal := strOrEmpty(profile.Tone) == "formal"
	tmpl := orderTemplatesFromKB(kb, formal, userText)

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
	forceNew := IsExplicitNewOrderStart(userText)
	if forceNew {
		if state == nil {
			fresh := orderState{ForceNewOrder: true, Step: "ask_product"}
			state = &fresh
		} else {
			state.ForceNewOrder = true
			state.PersistedOrderID = ""
		}
	}
	if state == nil && parseOrderRefFromMessage(userText) == "" && hasPurchaseIntent(userText, catalog) {
		if drafts, derr := loadDraftOrdersForContact(ctx, ts, tenantSchema, scope); derr == nil && len(drafts) > 1 {
			return send(orderAmendPickDraftReply(drafts), PathOrderFlow)
		}
	}
	if ref := parseOrderRefFromMessage(userText); ref != "" && (state == nil || forceNew) {
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
				pinned := orderStateFromPersistedDraft(o)
				s.setOrderState(ctx, tenantID, convo.ID, pinned)
				state = &pinned
				forceNew = false
			}
		}
	}

	if IsStructuredOrderList(userText) {
		// A list pasted into a live checkout joins the cart (recipient and address stay); only an
		// explicit "pesanan baru" starts over. It used to wipe the cart whatever the buyer meant.
		var cart *orderState
		if state != nil {
			if forceNew {
				s.clearOrderState(ctx, tenantID, convo.ID)
				state = nil
			} else {
				cart = state
			}
		}
		outcome := evaluateStructuredOrder(userText, catalog, formal, vctx)
		if outcome.Matched {
			if len(outcome.Lines) == 0 {
				if len(outcome.Unmatched) > 0 {
					return send(structuredOrderUnmatchedReply(formal, outcome.Unmatched), PathOrderFlow)
				}
			} else {
				if cart != nil {
					unmatched := outcome.Unmatched
					outcome = mergeStructuredIntoCart(*cart, outcome.Lines, func(st orderState) (orderState, string, bool) {
						return guardStructuredOrderStock(st, catalog, formal)
					})
					outcome.Unmatched = unmatched
				}
				if forceNew {
					outcome.State.ForceNewOrder = true
					outcome.State.PersistedOrderID = ""
				}
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
				if cart != nil {
					prompt = nextCheckoutPrompt(outcome.State, tmpl, buyerReplyLang(userText, contact))
				}
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
		if forceNew {
			st.ForceNewOrder = true
			if strings.TrimSpace(st.PersistedOrderID) != "" && IsExplicitNewOrderStart(userText) {
				st.PersistedOrderID = ""
			}
		}
		if res.State.Step == "ask_recipient" && st.CartReadyForDraft() {
			if orderID, needPick, pickList, err := persistDraftOrderEarly(ctx, ts, tenantSchema, convo.ID, convo.ContactID, st); err != nil {
				rlog.Warn("AI order: early draft persist failed", "err", err, "convoId", convo.ID)
			} else if needPick {
				s.setOrderState(ctx, tenantID, convo.ID, st)
				return send(orderAmendPickDraftReply(pickList), PathOrderFlow)
			} else if orderID != "" {
				st.PersistedOrderID = orderID
				st.ForceNewOrder = false
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

// cartLinesOf returns the cart's product lines: Items, or the single-product fields as one line.
// A single product still waiting for its quantity is a selection, not a cart line.
func cartLinesOf(st orderState) []orderLineState {
	if len(st.Items) > 0 {
		return append([]orderLineState(nil), st.Items...)
	}
	if st.Qty < 1 || (strings.TrimSpace(st.CatalogItemID) == "" && strings.TrimSpace(st.ProductName) == "") {
		return nil
	}
	return []orderLineState{{
		CatalogItemID: st.CatalogItemID, ExternalCode: st.ExternalCode, ProductName: st.ProductName,
		Size: st.Size, Color: st.Color, Qty: st.Qty, UnitPrice: st.UnitPrice,
		SellUnit: st.SellUnit, WarehouseID: st.WarehouseID,
	}}
}

// mergeStructuredIntoCart joins the lines of a pasted order list to the existing cart with
// MergeOrderLines (the same product adds quantity). Everything else the cart already holds,
// the recipient and the address included, is kept. The merged cart (not just the new lines) gets
// the variant check and the stock guard a fresh list gets, and the step to resume at: variants,
// then recipient, then address.
func mergeStructuredIntoCart(existing orderState, lines []orderLineState, stockGuard func(orderState) (orderState, string, bool)) structuredOrderFlowOutcome {
	out := structuredOrderFlowOutcome{Matched: true, Lines: lines}
	if len(lines) == 0 {
		return out
	}
	merged := existing
	merged.Items = MergeOrderLines(cartLinesOf(existing), lines)
	if len(merged.Items) > 0 {
		bf.ApplyLineToOrderState(&merged, merged.Items[0])
	}
	if !merged.StructuredLinesReady() {
		merged.Step = "ask_variant"
		out.State = merged
		out.NeedVariant = true
		return out
	}
	merged, reply, blocked := stockGuard(merged)
	if blocked {
		out.Blocked = true
		out.BlockReply = reply
		return out
	}
	switch {
	case strings.TrimSpace(merged.RecipientName) == "" || strings.TrimSpace(merged.RecipientPhone) == "":
		merged.Step = "ask_recipient"
	default:
		merged.Step = "ask_address_full"
	}
	out.State = merged
	return out
}

// nextCheckoutPrompt asks only for what a merged cart still lacks: variants, then the
// recipient, then the address. A cart with everything gets the "reply OK" confirmation.
func nextCheckoutPrompt(st orderState, tmpl orderFlowTemplates, lang salesbrain.Lang) string {
	switch {
	case !st.StructuredLinesReady():
		return tmpl.AskVariant
	case strings.TrimSpace(st.RecipientName) == "" || strings.TrimSpace(st.RecipientPhone) == "":
		return tmpl.AskRecipient
	case st.ShippingComplete():
		return salesbrain.T(salesbrain.TplResumeConfirm, lang)
	case strings.TrimSpace(st.Street) == "" && strings.TrimSpace(st.City) == "" && strings.TrimSpace(st.PostalCode) == "":
		return tmpl.AskAddressFull
	default:
		return tmpl.ClarifyAddress
	}
}
