package buyerflow

// Simulator — multi-turn WhatsApp buyer flow tanpa Redis/DB.
type Simulator struct {
	Profile *BusinessProfile
	Catalog []CatalogItem
	History []Message
	KB      []KBEntry
	Order   *OrderState
	ScopeKW []string
}

// TurnOutcome — hasil satu pesan masuk pembeli.
type TurnOutcome struct {
	Path      string
	Intent    SalesIntent
	Order     *OrderState
	Completed bool
	Canceled  bool
	Reply     string
	BrokeFlow bool
}

func (s *Simulator) inScope(userText string) bool {
	if s.ScopeKW == nil && s.Profile != nil {
		s.ScopeKW = businessScopeKeywords(s.Profile)
	}
	return IsWithinBusinessScope(userText, s.ScopeKW, nil)
}

func (s *Simulator) InScope(userText string) bool { return s.inScope(userText) }

func (s *Simulator) appendHistory(in, out string) {
	if in != "" {
		s.History = append(s.History, Message{Direction: "in", Body: in})
	}
	if out != "" {
		s.History = append(s.History, Message{Direction: "out", Body: out})
	}
}

// Turn memproses satu pesan pembeli (routing + FSM).
func (s *Simulator) Turn(userText string) TurnOutcome {
	out := TurnOutcome{}

	// Match autoreply.go: third-party buyer lookup denied before other routing.
	if IsThirdPartyBuyerLookup(userText) {
		out.Path = PathOrderLookupDenied
		out.Intent = SalesIntent{State: SalesStateOutOfScope, Topic: SalesTopicOrderStatus, Confidence: 0.95}
		out.Reply = thirdPartyBuyerLookupDeniedReply()
		s.appendHistory(userText, out.Reply)
		return out
	}

	// Match autoreply.go: status inquiry before greeting and cancel.
	if s.Order != nil && (IsCartRecapOrComplaint(userText, s.Catalog) ||
		PreferCheckoutRecapOverDBStatus(userText, CheckoutStateHasRecap(s.Order))) {
		formal := strOrEmpty(s.Profile.Tone) == "formal"
		out.Path = PathOrderFlow
		out.Intent = SalesIntent{State: SalesStateCheckout, Topic: SalesTopicGeneral, Confidence: 0.9}
		out.Reply = CartRecapReply(*s.Order, formal)
		out.Order = s.Order
		s.appendHistory(userText, out.Reply)
		return out
	}
	if IsOrderStatusInquiry(userText) || IsSelfBuyerOrderLookup(userText) || IsOrderRefStatusLookup(userText) {
		out.Path = PathOrderStatus
		out.Intent = SalesIntent{State: SalesStateConsulting, Topic: SalesTopicOrderStatus, Confidence: 0.9}
		s.appendHistory(userText, "")
		return out
	}

	// Match autoreply.go: greetings always win (clear draft order), tidak perlu in-scope.
	if IsGreetingLike(userText) {
		s.Order = nil
		out.Path = PathGreeting
		out.Intent = SalesIntent{State: SalesStateGreeting}
		out.Reply = GreetingReply(userText, strOrEmpty(s.Profile.Tone), "")
		s.appendHistory(userText, out.Reply)
		return out
	}

	// Match autoreply.go: status inquiry before cancel (clarification ≠ cancel command).
	if IsOrderStatusInquiry(userText) {
		out.Path = PathOrderStatus
		out.Intent = SalesIntent{State: SalesStateConsulting, Topic: SalesTopicOrderStatus, Confidence: 0.9}
		s.appendHistory(userText, "")
		return out
	}

	if IsOrderCancelRequest(userText) || IsDraftOrderCancelRequest(userText) {
		hadOrder := s.Order != nil
		s.Order = nil
		out.Canceled = true
		out.Path = PathOrderCancel
		if hadOrder {
			out.Reply = orderFlowCancelReply(strOrEmpty(s.Profile.Tone))
		}
		s.appendHistory(userText, out.Reply)
		return out
	}

	orderActive := s.Order != nil
	inScope := s.inScope(userText) || isCommerceDominant(userText)

	if orderActive {
		step := s.Order.Step
		if IsAddMoreItemsPolicyQuestion(userText) {
			formal := strOrEmpty(s.Profile.Tone) == "formal"
			out.Path = PathOrderFlow
			out.Intent = SalesIntent{State: SalesStateCheckout, Topic: SalesTopicGeneral, Confidence: 0.9}
			out.Reply = AddMoreItemsPolicyReply(formal, s.Order)
			out.Order = s.Order
			s.appendHistory(userText, out.Reply)
			return out
		}
		if ShouldBreakOrderFlow(userText, step, s.Catalog) {
			s.Order = nil
			out.BrokeFlow = true
			orderActive = false
			out.Path = PathConsulting
			if IsUserSalesCorrection(userText) {
				out.Intent = SalesIntent{State: SalesStateCorrection, Topic: SalesTopicGeneral, Confidence: 0.92}
				out.Reply = orderFlowLoopBreakReply(strOrEmpty(s.Profile.Tone) == "formal")
			} else {
				out.Intent = ResolveSalesIntent(userText, s.History, false, s.inScope(userText), s.Profile, s.Catalog)
			}
			s.appendHistory(userText, out.Reply)
			return out
		} else {
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
			out.Intent = SalesIntent{State: SalesStateCheckout, Topic: SalesTopicGeneral, Confidence: 0.9}
			if res.Cleared {
				s.Order = nil
			} else {
				s.Order = res.State
			}
			out.Order = s.Order
			s.appendHistory(userText, out.Reply)
			return out
		}
	}

	intent := ResolveSalesIntent(userText, s.History, orderActive, inScope, s.Profile, s.Catalog)
	out.Intent = intent

	if inScope && (IsOrderRevisionMessage(userText) ||
		(mentionsOrderQty(userText) && IsActiveCheckoutFromHistory(s.History, userText))) {
		res := AdvanceOrderFlow(OrderFlowInput{
			UserText: userText,
			State:    s.Order,
			Catalog:  s.Catalog,
			History:  s.History,
			Profile:  s.Profile,
			KB:       s.KB,
			ScopeKW:  s.ScopeKW,
		}, func(st OrderState) (string, error) { return "sim-order", nil })
		out.Path = res.Path
		out.Reply = res.Reply
		out.Completed = res.Completed
		if res.Cleared {
			s.Order = nil
		} else {
			s.Order = res.State
		}
		out.Order = s.Order
		s.appendHistory(userText, out.Reply)
		return out
	}

	cr := salesIntentToClassifier(intent)
	if cr.Label == "order_intent" || hasPurchaseIntent(userText, s.Catalog) {
		res := AdvanceOrderFlow(OrderFlowInput{
			UserText: userText,
			State:    s.Order,
			Catalog:  s.Catalog,
			History:  s.History,
			Profile:  s.Profile,
			KB:       s.KB,
			ScopeKW:  s.ScopeKW,
		}, func(st OrderState) (string, error) { return "sim-order", nil })
		out.Path = res.Path
		out.Reply = res.Reply
		out.Completed = res.Completed
		if res.Cleared {
			s.Order = nil
		} else if res.State != nil {
			s.Order = res.State
		}
		out.Order = s.Order
		s.appendHistory(userText, out.Reply)
		return out
	}

	if IsRecipientPolicyQuestion(userText) {
		formal := strOrEmpty(s.Profile.Tone) == "formal"
		out.Path = PathRecipientPolicy
		out.Reply = replyRecipientPolicyQuestion(userText, s.KB, formal)
		s.appendHistory(userText, out.Reply)
		return out
	}

	if inScope && IsAddMoreItemsPolicyQuestion(userText) {
		formal := strOrEmpty(s.Profile.Tone) == "formal"
		out.Path = PathConsulting
		out.Intent = SalesIntent{State: SalesStateConsulting, Topic: SalesTopicGeneral, Confidence: 0.9}
		out.Reply = AddMoreItemsPolicyReply(formal, s.Order)
		s.appendHistory(userText, out.Reply)
		return out
	}

	if IsPaymentQuestion(userText) {
		if ans, ok := tryPaymentFAQAnswer(userText, s.KB); ok {
			out.Path = PathPaymentFAQ
			out.Intent = SalesIntent{State: SalesStateConsulting, Topic: SalesTopicGeneral, Confidence: 0.9}
			out.Reply = ans
			s.appendHistory(userText, out.Reply)
			return out
		}
	}

	if catReply, ok := replyFromBusinessCatalog(userText, s.Profile, s.Catalog, s.History, nil); ok {
		out.Path = PathCatalogDB
		out.Reply = catReply
		s.appendHistory(userText, out.Reply)
		return out
	}

	if inScope {
		formal := strOrEmpty(s.Profile.Tone) == "formal"
		if shipReply, ok := TryShippingFAQReply(userText, s.Profile, s.KB, formal); ok {
			out.Path = PathShippingFAQ
			out.Reply = shipReply
			s.appendHistory(userText, out.Reply)
			return out
		}
	}

	if inScope {
		if ans, ok := tryFAQDirectAnswer(userText, s.KB); ok {
			out.Path = PathFAQDirect
			out.Reply = ans
			s.appendHistory(userText, out.Reply)
			return out
		}
	}

	out.Path = PathConsulting
	out.Reply = "consulting"
	s.appendHistory(userText, out.Reply)
	return out
}

// RunScript menjalankan urutan pesan dan mengembalikan outcome terakhir.
func (s *Simulator) RunScript(msgs ...string) []TurnOutcome {
	outcomes := make([]TurnOutcome, 0, len(msgs))
	for _, m := range msgs {
		outcomes = append(outcomes, s.Turn(m))
	}
	return outcomes
}

func newOmahSimulator() *Simulator {
	p := omahProfile()
	return &Simulator{
		Profile: p,
		Catalog: omahCatalog(),
		KB:      omahKB(),
		ScopeKW: businessScopeKeywords(p),
	}
}

func omahKB() []KBEntry {
	catPay := "Nomor Rekening"
	catShip := "Pengiriman"
	return []KBEntry{
		{
			Question: "Nomor Rekening",
			Answer:   "BCA 110220330 atas nama Omah Apparel\nMandiri 311211111 atas nama Omah Apparel",
			Category: &catPay,
			IsActive: true,
		},
		{
			Question: "berapa lama pengiriman",
			Answer:   "Estimasi pengiriman 2-3 hari kerja via JNE.",
			Category: &catShip,
			IsActive: true,
		},
		{
			Question: "bisa kirim ke luar kota",
			Answer:   "Bisa kak, kami kirim ke luar kota dan seluruh Indonesia.",
			Category: &catShip,
			IsActive: true,
		},
	}
}

func omahPaymentKB() []KBEntry {
	return omahKB()[:1]
}

func fullAddressBlock() string {
	return "Jalan: Jl Melati 10\nKota/Kab: Jakarta Selatan\nProvinsi: DKI Jakarta\nKode pos: 12345"
}

func recipientBlock(name, phone string) string {
	return "Nama: " + name + "\nHP: " + phone
}
