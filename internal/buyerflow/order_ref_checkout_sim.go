package buyerflow

// handleOrderRefCheckoutContinuation — mirror ai/autoreply guard (tanpa DB).
func (s *Simulator) handleOrderRefCheckoutContinuation(userText string) (TurnOutcome, bool) {
	if !IsOrderRefCheckoutContinuation(userText) {
		return TurnOutcome{}, false
	}
	formal := strOrEmpty(s.Profile.Tone) == "formal"
	refs := ParseAllOrderRefsFromMessage(userText)
	out := TurnOutcome{
		Intent: SalesIntent{State: SalesStateCheckout, Topic: SalesTopicGeneral, Confidence: 0.9},
		Order:  s.Order,
	}
	if len(refs) > 1 {
		out.Path = PathOrderFlow
		out.Reply = OrderRefCheckoutPickOneReply(refs, formal)
		return out, true
	}
	ref := refs[0]
	if s.SimOrderRefStatus != nil {
		if status, ok := s.SimOrderRefStatus[ref]; ok && status != "" && status != "draft" {
			out.Path = PathOrderFlow
			out.Reply = OrderRefCheckoutNonDraftReply(formal, status, ref)
			return out, true
		}
	}
	if s.Order == nil || !OrderRefMatchesPersistedID(ref, s.Order.PersistedOrderID) {
		out.Path = PathOrderFlow
		out.Reply = orderRefCheckoutNotFoundReply(ref)
		return out, true
	}
	res := AdvanceOrderFlow(OrderFlowInput{
		UserText: userText,
		State:    s.Order,
		Catalog:  s.Catalog,
		History:  s.History,
		Profile:  s.Profile,
		KB:       s.KB,
		ScopeKW:  s.ScopeKW,
	}, func(st OrderState) (string, error) {
		return "sim-" + st.ProductName, nil
	})
	out.Path = res.Path
	out.Reply = res.Reply
	out.Completed = res.Completed
	if res.Cleared {
		s.Order = nil
	} else {
		s.Order = res.State
	}
	out.Order = s.Order
	return out, true
}
