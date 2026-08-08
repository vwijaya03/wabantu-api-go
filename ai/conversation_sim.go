package ai

// ConversationSimulator — multi-turn WhatsApp buyer flow tanpa Redis/DB.
type ConversationSimulator struct {
	Profile *dbBusinessProfile
	Catalog []dbCatalogItem
	History []dbMessage
	KB      []dbKBEntry
	Order   *orderState
	ScopeKW []string
}

// TurnOutcome — hasil satu pesan masuk pembeli.
type TurnOutcome struct {
	Path      string
	Intent    SalesIntent
	Order     *orderState
	Completed bool
	Canceled  bool
	Reply     string
	BrokeFlow bool
}

func (s *ConversationSimulator) inScope(userText string) bool {
	if s.ScopeKW == nil && s.Profile != nil {
		s.ScopeKW = businessScopeKeywords(s.Profile)
	}
	return IsWithinBusinessScope(userText, s.ScopeKW, nil)
}

func (s *ConversationSimulator) appendHistory(in, out string) {
	if in != "" {
		s.History = append(s.History, dbMessage{Direction: "in", Body: in})
	}
	if out != "" {
		s.History = append(s.History, dbMessage{Direction: "out", Body: out})
	}
}

// Turn memproses satu pesan pembeli (routing + FSM).
func (s *ConversationSimulator) Turn(userText string) TurnOutcome {
	out := TurnOutcome{}
	catalog := s.Catalog

	// Match autoreply.go: third-party buyer lookup denied before other routing.
	if IsThirdPartyBuyerLookup(userText) {
		out.Path = PathOrderLookupDenied
		out.Intent = SalesIntent{State: SalesStateOutOfScope, Topic: SalesTopicOrderStatus, Confidence: 0.95}
		out.Reply = thirdPartyBuyerLookupDeniedReply()
		s.appendHistory(userText, out.Reply)
		return out
	}

	// Match autoreply.go: status inquiry before greeting and cancel.
	if (IsOrderStatusInquiry(userText) || IsSelfBuyerOrderLookup(userText) || IsOrderRefStatusLookup(userText)) && !wantsOrderContextFromHistory(userText) {
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

	if IsPaymentQuestion(userText) {
		if ans, ok := tryPaymentFAQAnswer(userText, s.KB); ok {
			out.Path = PathPaymentFAQ
			out.Intent = SalesIntent{State: SalesStateConsulting, Topic: SalesTopicGeneral, Confidence: 0.9}
			out.Reply = ans
			s.appendHistory(userText, out.Reply)
			return out
		}
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

	if IsOrderStatusInquiry(userText) || IsSelfBuyerOrderLookup(userText) || IsOrderRefStatusLookup(userText) {
		out.Path = PathOrderStatus
		out.Intent = SalesIntent{State: SalesStateConsulting, Topic: SalesTopicOrderStatus, Confidence: 0.9}
		s.appendHistory(userText, "")
		return out
	}

	clearedOrderForCorrection := false
	orderActive := s.Order != nil

	// Active order flow — match autoreply.go: break clears state then fall through.
	if orderActive {
		step := s.Order.Step
		if ShouldBreakOrderFlow(userText, step, catalog) {
			clearedOrderForCorrection = IsUserSalesCorrection(userText)
			s.Order = nil
			orderActive = false
		} else {
			res := AdvanceOrderFlow(OrderFlowInput{
				UserText: userText,
				State:    s.Order,
				Catalog:  catalog,
				History:  s.History,
				Profile:  s.Profile,
				KB:       s.KB,
				ScopeKW:  s.ScopeKW,
			}, func(st orderState) (string, error) {
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

	inScope := resolveSimulatorInScope(userText, s.History, s.ScopeKW)
	intent, classifier := resolveInboundClassifier(userText, s.History, orderActive, inScope, s.Profile, catalog)
	out.Intent = intent

	if shouldEnterOrderFlow(userText, s.History, inScope, classifier, catalog) {
		res := AdvanceOrderFlow(OrderFlowInput{
			UserText: userText,
			State:    s.Order,
			Catalog:  catalog,
			History:  s.History,
			Profile:  s.Profile,
			KB:       s.KB,
			ScopeKW:  s.ScopeKW,
		}, func(st orderState) (string, error) { return "sim-order", nil })
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

	if inScope && IsRecipientPolicyQuestion(userText) {
		formal := strOrEmpty(s.Profile.Tone) == "formal"
		out.Path = PathRecipientPolicy
		out.Reply = replyRecipientPolicyQuestion(userText, s.KB, formal)
		s.appendHistory(userText, out.Reply)
		return out
	}

	if inScope {
		if catReply, ok := replyFromBusinessCatalog(userText, s.Profile, catalog, s.History); ok {
			if clearedOrderForCorrection {
				catReply = prependSalesCorrection(strOrEmpty(s.Profile.Tone) == "formal", catReply)
			}
			out.Path = PathCatalogDB
			out.Reply = catReply
			s.appendHistory(userText, out.Reply)
			return out
		}
		if clearedOrderForCorrection {
			formal := strOrEmpty(s.Profile.Tone) == "formal"
			out.Path = PathConsulting
			out.Reply = salesCorrectionReply(formal)
			s.appendHistory(userText, out.Reply)
			return out
		}
	}

	if classifier.Label == "sensitive_escalate" {
		out.Path = PathEscalate
		out.Reply = "escalate"
		s.appendHistory(userText, out.Reply)
		return out
	}
	if classifier.Label == "out_of_scope" {
		out.Path = PathOutOfScope
		out.Reply = outOfScopeReply(s.Profile)
		s.appendHistory(userText, out.Reply)
		return out
	}

	if inScope && IsCatalogBrowsingIntent(userText) {
		if catReply, ok := replyFromBusinessCatalog(userText, s.Profile, catalog, s.History); ok {
			out.Path = PathCatalogDB
			out.Reply = catReply
			s.appendHistory(userText, out.Reply)
			return out
		}
	}

	if inScope && IsCasualPraiseLike(userText) {
		formal := strOrEmpty(s.Profile.Tone) == "formal"
		out.Path = PathConsulting
		out.Reply = casualPraiseReply(formal)
		s.appendHistory(userText, out.Reply)
		return out
	}

	out.Path = PathConsulting
	out.Reply = "consulting"
	s.appendHistory(userText, out.Reply)
	return out
}

// RunScript menjalankan urutan pesan dan mengembalikan outcome terakhir.
func (s *ConversationSimulator) RunScript(msgs ...string) []TurnOutcome {
	outcomes := make([]TurnOutcome, 0, len(msgs))
	for _, m := range msgs {
		outcomes = append(outcomes, s.Turn(m))
	}
	return outcomes
}

func newOmahSimulator() *ConversationSimulator {
	p := omahProfile()
	return &ConversationSimulator{
		Profile: p,
		Catalog: omahCatalog(),
		KB:      omahPaymentKB(),
		ScopeKW: businessScopeKeywords(p),
	}
}

func omahPaymentKB() []dbKBEntry {
	cat := "Nomor Rekening"
	return []dbKBEntry{{
		Question: "Nomor Rekening",
		Answer:   "BCA 110220330 atas nama Omah Apparel\nMandiri 311211111 atas nama Omah Apparel",
		Category: &cat,
		IsActive: true,
	}}
}

func fullAddressBlock() string {
	return "Jalan: Jl Melati 10\nKota/Kab: Jakarta Selatan\nProvinsi: DKI Jakarta\nKode pos: 12345"
}

func recipientBlock(name, phone string) string {
	return "Nama: " + name + "\nHP: " + phone
}
