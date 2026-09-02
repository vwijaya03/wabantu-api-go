package buyerflow

// OrderFlowInput — pure order FSM step (no Redis/DB/WA).
type OrderFlowInput struct {
	UserText string
	State    *OrderState
	Catalog  []CatalogItem
	History  []Message
	Profile  *BusinessProfile
	KB       []KBEntry
	ScopeKW  []string
}

// OrderFlowResult — outcome of one FSM advance.
type OrderFlowResult struct {
	State        *OrderState
	Reply        string
	Path         string
	Completed    bool
	Cleared      bool
	CatalogReply bool
	OrderID      string
}

type persistOrderFunc = PersistOrderFunc

func buildOrderFlowReply(st OrderState, prompt string, catalog []CatalogItem) string {
	line := catalogConfirmLine(st)
	if line != "" {
		prompt = line + "\n\n" + prompt
	}
	if st.Qty > 0 && st.ProductComplete() {
		if upsell := formatUpsellBlock(st, catalog); upsell != "" {
			prompt += "\n\n" + upsell
		}
	}
	return prompt
}

// AdvanceOrderFlow advances checkout FSM without side effects.
func AdvanceOrderFlow(in OrderFlowInput, persist persistOrderFunc) OrderFlowResult {
	userText := in.UserText
	catalog := in.Catalog
	history := in.History
	profile := in.Profile
	state := in.State

	formal := profile != nil && strOrEmpty(profile.Tone) == "formal"
	tmpl := orderTemplatesFromKB(in.KB, formal)

	if IsOffBusinessProductRequest(userText, in.ScopeKW) {
		return OrderFlowResult{Cleared: true, Path: PathOutOfScope, Reply: outOfScopeReply(profile)}
	}

	hints := parseOrderHints(userText)
	copyBase := func(st OrderState) OrderState {
		st = normalizeOrderState(st)
		if state != nil {
			base := normalizeOrderState(*state)
			base.Step = st.Step
			if st.CatalogItemID != "" {
				base.CatalogItemID = st.CatalogItemID
			}
			if st.ProductName != "" {
				base.ProductName = st.ProductName
			}
			if st.Size != "" {
				base.Size = st.Size
			}
			if st.Color != "" {
				base.Color = st.Color
			}
			if st.Qty > 0 {
				base.Qty = st.Qty
			}
			if st.WarehouseID != "" {
				base.WarehouseID = st.WarehouseID
			}
			if st.UnitPrice > 0 {
				base.UnitPrice = st.UnitPrice
			}
			if st.ExternalCode != "" {
				base.ExternalCode = st.ExternalCode
			}
			if st.SellUnit != "" {
				base.SellUnit = st.SellUnit
			}
			if st.RecipientName != "" {
				base.RecipientName = st.RecipientName
			}
			if st.RecipientPhone != "" {
				base.RecipientPhone = st.RecipientPhone
			}
			if st.Street != "" {
				base.Street = st.Street
			}
			if st.City != "" {
				base.City = st.City
			}
			if st.Province != "" {
				base.Province = st.Province
			}
			if st.PostalCode != "" {
				base.PostalCode = st.PostalCode
			}
			return base
		}
		return st
	}

	tryCatalogEscape := func() (OrderFlowResult, bool) {
		if IsOrderRevisionMessage(userText) {
			return OrderFlowResult{}, false
		}
		if !IsCatalogBrowsingIntent(userText) && !isGeneralStoreCatalogQuestion(userText) {
			return OrderFlowResult{}, false
		}
		if catReply, ok := replyFromBusinessCatalog(userText, profile, catalog, history, nil); ok {
			return OrderFlowResult{Cleared: true, CatalogReply: true, Path: PathCatalogDB, Reply: catReply}, true
		}
		return OrderFlowResult{}, false
	}

	if state != nil {
		if res, ok := tryCatalogEscape(); ok {
			return res
		}
	}

	if state == nil && (IsOrderRevisionMessage(userText) || mentionsOrderQty(userText)) {
		if match := resolveOrderProductMatch(userText, history, catalog); match != nil {
			st := OrderState{Step: "ask_variant"}
			applyCatalogMatch(&st, match)
			if q, ok := parseOrderQty(userText); ok {
				st.Qty = q
			}
			inferVariantFromProductName(&st)
			if st.VariantComplete() && st.Qty > 0 {
				st, reply, blocked := guardOrderQtyStep(st, catalog, formal, "ask_qty")
				if blocked {
					return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: reply}
				}
				st.Step = "ask_recipient"
				return OrderFlowResult{
					State: &st, Path: PathOrderFlow,
					Reply: buildOrderFlowReply(st, tmpl.AskRecipient, catalog),
				}
			}
			if st.Qty > 0 {
				st.Step = "ask_qty"
				return OrderFlowResult{
					State: &st, Path: PathOrderFlow,
					Reply: buildOrderFlowReply(st, tmpl.AskQty, catalog),
				}
			}
		}
	}

	if state == nil {
		st := OrderState{Step: "ask_product"}
		if match := matchCatalogItem(userText, catalog); match != nil {
			applyCatalogMatch(&st, match)
			sz, cl := parseSizeAndColor(userText)
			if sz != "" {
				st.Size = sz
			}
			if cl != "" {
				st.Color = cl
			}
			if hints.HasQty {
				st.Qty = hints.Qty
			}
			if st.VariantComplete() {
				if st.Qty > 0 {
					st, reply, blocked := guardOrderQtyStep(st, catalog, formal, "ask_qty")
				if blocked {
						return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: reply}
					}
					st.Step = "ask_recipient"
					return OrderFlowResult{
						State: &st, Path: PathOrderFlow,
						Reply: buildOrderFlowReply(st, tmpl.AskRecipient, catalog),
					}
				}
				st.Step = "ask_qty"
				return OrderFlowResult{
					State: &st, Path: PathOrderFlow,
					Reply: buildOrderFlowReply(st, tmpl.AskQty, catalog),
				}
			}
			st.Step = "ask_variant"
			return OrderFlowResult{
				State: &st, Path: PathOrderFlow,
				Reply: buildOrderFlowReply(st, tmpl.AskVariant, catalog),
			}
		}
		msg := tmpl.AskProduct
		if picker := formatCatalogPicker(catalog, 6); picker != "" {
			msg += "\n\nContoh produk:\n" + picker
		}
		return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: msg}
	}

	stateNorm := normalizeOrderState(*state)

	if IsOrderTotalRequest(userText) && stateNorm.ProductComplete() && stateNorm.Qty > 0 {
		msg := formatOrderSummary(stateNorm)
		if upsell := formatUpsellBlock(stateNorm, catalog); upsell != "" {
			msg += "\n\n" + upsell
		}
		return OrderFlowResult{State: &stateNorm, Path: PathOrderFlow, Reply: msg}
	}

	switch stateNorm.Step {
	case "ask_product":
		if IsOffBusinessProductRequest(userText, in.ScopeKW) {
			return OrderFlowResult{Cleared: true, Path: PathOutOfScope, Reply: outOfScopeReply(profile)}
		}
		st := copyBase(OrderState{Step: "ask_product"})
		match := matchCatalogItem(userText, catalog)
		if match == nil {
			msg := "Maaf kak, produknya belum ketemu di katalog. Sebut nama produk yang ada di katalog ya."
			if picker := formatCatalogPicker(catalog, 6); picker != "" {
				msg += "\n\n" + picker
			}
			return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: msg}
		}
		applyCatalogMatch(&st, match)
		sz, cl := parseSizeAndColor(userText)
		if sz != "" {
			st.Size = sz
		}
		if cl != "" {
			st.Color = cl
		}
		if hints.HasQty {
			st.Qty = hints.Qty
		}
		if !st.VariantComplete() {
			st.Step = "ask_variant"
			return OrderFlowResult{
				State: &st, Path: PathOrderFlow,
				Reply: buildOrderFlowReply(st, tmpl.AskVariant, catalog),
			}
		}
		if st.Qty < 1 {
			st.Step = "ask_qty"
			return OrderFlowResult{
				State: &st, Path: PathOrderFlow,
				Reply: buildOrderFlowReply(st, tmpl.AskQty, catalog),
			}
		}
		st, reply, blocked := guardOrderQtyStep(st, catalog, formal, "ask_qty")
				if blocked {
			return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: reply}
		}
		st.Step = "ask_recipient"
		return OrderFlowResult{
			State: &st, Path: PathOrderFlow,
			Reply: buildOrderFlowReply(st, tmpl.AskRecipient, catalog),
		}

	case "ask_variant":
		st := copyBase(stateNorm)
		if hints.HasQty {
			st.Qty = hints.Qty
		} else if q, ok := parseOrderQty(userText); ok {
			st.Qty = q
		}
		inferVariantFromProductName(&st)
		if !catalogItemNeedsVariant(&CatalogItem{Name: st.ProductName, ExternalCode: st.ExternalCode}) {
			if st.Qty < 1 {
				if q, ok := parseOrderQty(userText); ok {
					st.Qty = q
				}
			}
			if st.Qty < 1 {
				st.Step = "ask_qty"
				return OrderFlowResult{
					State: &st, Path: PathOrderFlow,
					Reply: buildOrderFlowReply(st, tmpl.AskQty, catalog),
				}
			}
			st, reply, blocked := guardOrderQtyStep(st, catalog, formal, "ask_qty")
				if blocked {
				return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: reply}
			}
			st.Step = "ask_recipient"
			return OrderFlowResult{
				State: &st, Path: PathOrderFlow,
				Reply: buildOrderFlowReply(st, tmpl.AskRecipient, catalog),
			}
		}
		sz, cl := parseSizeAndColor(userText)
		if sz != "" {
			st.Size = sz
		}
		if cl != "" {
			st.Color = cl
		}
		if st.VariantComplete() && st.Qty > 0 {
			st, reply, blocked := guardOrderQtyStep(st, catalog, formal, "ask_qty")
				if blocked {
				return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: reply}
			}
			st.Step = "ask_recipient"
			return OrderFlowResult{
				State: &st, Path: PathOrderFlow,
				Reply: buildOrderFlowReply(st, tmpl.AskRecipient, catalog),
			}
		}
		if !st.VariantComplete() {
			if IsCatalogBrowsingIntent(userText) || isGeneralStoreCatalogQuestion(userText) {
				if catReply, ok := replyFromBusinessCatalog(userText, profile, catalog, history, nil); ok {
					return OrderFlowResult{Cleared: true, CatalogReply: true, Path: PathCatalogDB, Reply: catReply}
				}
			}
			if wouldRepeatOutbound(history, tmpl.AskVariant) {
				if catReply, ok := replyFromBusinessCatalog(userText, profile, catalog, history, nil); ok {
					return OrderFlowResult{Cleared: true, CatalogReply: true, Path: PathCatalogDB, Reply: catReply}
				}
				return OrderFlowResult{Cleared: true, Path: PathOrderFlow, Reply: orderFlowLoopBreakReply(formal)}
			}
			return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: tmpl.AskVariant}
		}
		if st.Qty < 1 {
			st.Step = "ask_qty"
			return OrderFlowResult{
				State: &st, Path: PathOrderFlow,
				Reply: buildOrderFlowReply(st, tmpl.AskQty, catalog),
			}
		}
		st, reply, blocked := guardOrderQtyStep(st, catalog, formal, "ask_qty")
				if blocked {
			return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: reply}
		}
		st.Step = "ask_recipient"
		return OrderFlowResult{
			State: &st, Path: PathOrderFlow,
			Reply: buildOrderFlowReply(st, tmpl.AskRecipient, catalog),
		}

	case "ask_qty":
		st := copyBase(stateNorm)
		if handled, reply := TryAppendItemsDuringCheckout(&st, userText, catalog, tmpl, formal); handled {
			return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: reply}
		}
		qty := 0
		if hints.HasQty {
			qty = hints.Qty
		} else if q, ok := parseOrderQty(userText); ok {
			qty = q
		}
		if qty < 1 {
			return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: tmpl.ClarifyQty}
		}
		st.Qty = qty
		st, reply, blocked := guardOrderQtyStep(st, catalog, formal, "ask_qty")
				if blocked {
			return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: reply}
		}
		st.Step = "ask_recipient"
		return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: tmpl.AskRecipient}

	case "ask_recipient":
		st := copyBase(stateNorm)
		if handled, reply := TryAppendItemsDuringCheckout(&st, userText, catalog, tmpl, formal); handled {
			return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: reply}
		}
		if tryApplyQtyRevision(&st, userText) {
			st, reply, blocked := guardOrderQtyStep(st, catalog, formal, "ask_qty")
				if blocked {
				return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: reply}
			}
			return OrderFlowResult{
				State: &st, Path: PathOrderFlow,
				Reply: buildOrderFlowReply(st, tmpl.AskRecipient, catalog),
			}
		}
		if tryApplyProductRevision(&st, userText, catalog) {
			return OrderFlowResult{
				State: &st, Path: PathOrderFlow,
				Reply: buildOrderFlowReply(st, tmpl.AskRecipient, catalog),
			}
		}
		mergeShippingText(&st, userText)
		name, phone := parseRecipientLine(userText)
		if name != "" {
			st.RecipientName = name
		}
		if phone != "" {
			st.RecipientPhone = phone
		}
		if st.RecipientName == "" || st.RecipientPhone == "" {
			return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: tmpl.AskRecipient}
		}
		if st.ShippingComplete() {
			if missing := missingOrderDataPrompt(st, tmpl); missing != "" {
				return OrderFlowResult{
					State: &st, Path: PathOrderFlow,
					Reply: buildOrderFlowReply(st, missing, catalog),
				}
			}
			st, reply, blocked := guardOrderQtyStep(st, catalog, formal, "ask_qty")
				if blocked {
				return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: reply}
			}
			if persist != nil {
				orderID, err := persist(st)
				if err != nil {
					return OrderFlowResult{
						State: &st, Path: PathOrderFlow,
						Reply: buildOrderFlowReply(st, tmpl.RetryStep, catalog),
					}
				}
				return OrderFlowResult{
					Cleared: true, Completed: true, OrderID: orderID, Path: PathOrderFlow,
					Reply: orderCompleteMessageWithRef(orderID, st, tmpl),
				}
			}
			return OrderFlowResult{
				Cleared: true, Completed: true, OrderID: "sim-order-id", Path: PathOrderFlow,
				Reply: orderCompleteMessageWithRef("sim-order-id", st, tmpl),
			}
		}
		st.Step = "ask_address_full"
		return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: tmpl.AskAddressFull}

	case "ask_address", "ask_address_full":
		st := copyBase(stateNorm)
		if handled, reply := TryAppendItemsDuringCheckout(&st, userText, catalog, tmpl, formal); handled {
			return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: reply}
		}
		if tryApplyQtyRevision(&st, userText) {
			st, reply, blocked := guardOrderQtyStep(st, catalog, formal, "ask_qty")
				if blocked {
				return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: reply}
			}
			return OrderFlowResult{
				State: &st, Path: PathOrderFlow,
				Reply: buildOrderFlowReply(st, tmpl.AskRecipient, catalog),
			}
		}
		mergeShippingText(&st, userText)
		if !st.ShippingComplete() {
			return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: tmpl.ClarifyAddress}
		}
		if missing := missingOrderDataPrompt(st, tmpl); missing != "" {
			return OrderFlowResult{
				State: &st, Path: PathOrderFlow,
				Reply: buildOrderFlowReply(st, missing, catalog),
			}
		}
		st, reply, blocked := guardOrderQtyStep(st, catalog, formal, "ask_qty")
				if blocked {
			return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: reply}
		}
		if persist != nil {
			orderID, err := persist(st)
			if err != nil {
				return OrderFlowResult{
					State: &st, Path: PathOrderFlow,
					Reply: buildOrderFlowReply(st, tmpl.RetryStep, catalog),
				}
			}
			return OrderFlowResult{
				Cleared: true, Completed: true, OrderID: orderID, Path: PathOrderFlow,
				Reply: orderCompleteMessageWithRef(orderID, st, tmpl),
			}
		}
		return OrderFlowResult{
			Cleared: true, Completed: true, OrderID: "sim-order-id", Path: PathOrderFlow,
			Reply: orderCompleteMessageWithRef("sim-order-id", st, tmpl),
		}
	}

	return OrderFlowResult{State: &stateNorm, Path: PathOrderFlow, Reply: tmpl.RetryStep}
}
