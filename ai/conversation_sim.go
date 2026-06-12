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

	if IsGreetingLike(userText) && s.inScope(userText) {
		s.Order = nil
		out.Path = PathGreeting
		out.Intent = SalesIntent{State: SalesStateGreeting}
		out.Reply = GreetingReply(userText, strOrEmpty(s.Profile.Tone), "")
		s.appendHistory(userText, out.Reply)
		return out
	}

	if IsOrderCancelRequest(userText) {
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
	inScope := s.inScope(userText)

	if orderActive {
		step := s.Order.Step
		if ShouldBreakOrderFlow(userText, step) {
			s.Order = nil
			out.BrokeFlow = true
			orderActive = false
		} else {
			res := AdvanceOrderFlow(OrderFlowInput{
				UserText: userText,
				State:    s.Order,
				Catalog:  s.Catalog,
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
		}, func(st orderState) (string, error) { return "sim-order", nil })
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
	if cr.Label == "order_intent" || HasPurchaseIntent(userText) {
		res := AdvanceOrderFlow(OrderFlowInput{
			UserText: userText,
			State:    s.Order,
			Catalog:  s.Catalog,
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

	if catReply, ok := replyFromBusinessCatalog(userText, s.Profile, s.Catalog, s.History); ok {
		out.Path = PathCatalogDB
		out.Reply = catReply
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
		ScopeKW: businessScopeKeywords(p),
	}
}

func fullAddressBlock() string {
	return "Jalan: Jl Melati 10\nKota/Kab: Jakarta Selatan\nProvinsi: DKI Jakarta\nKode pos: 12345"
}

func recipientBlock(name, phone string) string {
	return "Nama: " + name + "\nHP: " + phone
}
