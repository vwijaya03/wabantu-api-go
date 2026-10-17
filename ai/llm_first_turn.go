package ai

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	bf "encore.app/wabantu/internal/buyerflow"
	"encore.app/wabantu/internal/salesbrain"
)

// Pure helpers of the LLM-first I/O shell (llm_first.go): everything here maps already-loaded
// data into the salesbrain input types or reads them back. Nothing touches Redis, the database,
// the network or the clock, so each helper is unit-tested without Encore infrastructure.

// ── time ─────────────────────────────────────────────────────────────────────

// lfWIB is the tenant timezone for greetings and the "now" line of the prompt. Jakarta has no
// daylight saving, so the fixed zone is exact when tzdata is missing from the container.
var lfWIB = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Jakarta"); err == nil {
		return loc
	}
	return time.FixedZone("WIB", 7*3600)
}()

// greetingFor is the time-of-day word of the prompt (STATE.salam) in Jakarta time:
// 05-11 pagi, 11-15 siang, 15-19 sore, otherwise malam. Same boundaries as the legacy greeting.
func greetingFor(t time.Time) string {
	switch h := t.In(lfWIB).Hour(); {
	case h >= 5 && h < 11:
		return "pagi"
	case h >= 11 && h < 15:
		return "siang"
	case h >= 15 && h < 19:
		return "sore"
	}
	return "malam"
}

var (
	lfDayNamesID   = [...]string{"Minggu", "Senin", "Selasa", "Rabu", "Kamis", "Jumat", "Sabtu"}
	lfMonthNamesID = [...]string{"Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}
)

// formatNowID renders STATE.waktu, e.g. "Sabtu 17 Okt 2026 14:20 WIB".
func formatNowID(t time.Time) string {
	t = t.In(lfWIB)
	return fmt.Sprintf("%s %d %s %d %02d:%02d WIB",
		lfDayNamesID[t.Weekday()], t.Day(), lfMonthNamesID[t.Month()-1], t.Year(), t.Hour(), t.Minute())
}

// ── history ──────────────────────────────────────────────────────────────────

// lfPastHistory cuts the current inbound message (or the whole coalesced burst it stands for) off
// the end of the loaded history: the message that triggered the reply is already stored, and the
// prompt carries it separately. It reuses dropTrailingCurrentInbound so the rule is the legacy one.
func lfPastHistory(history []dbMessage, currentText string) []dbMessage {
	msgs := make([]HistoryMessage, len(history))
	for i, m := range history {
		msgs[i] = HistoryMessage{Author: m.Author, Body: m.Body, Type: m.Type}
	}
	return history[:len(dropTrailingCurrentInbound(msgs, currentText))]
}

// historyTurnsFrom maps the stored conversation to prompt turns, oldest first, without the current
// message: contact → buyer, human → staff, ai and system (our own canned replies) → shop. A message
// without text (an image, a voice note) shows as "[type]" so the model sees that something was sent.
func historyTurnsFrom(history []dbMessage, currentText string) []salesbrain.HistoryTurn {
	past := lfPastHistory(history, currentText)
	turns := make([]salesbrain.HistoryTurn, 0, len(past))
	for _, m := range past {
		text := strings.TrimSpace(m.Body)
		if text == "" {
			if m.Type == "" || m.Type == "text" {
				continue
			}
			text = "[" + m.Type + "]"
		}
		role := salesbrain.RoleShop
		switch m.Author {
		case "contact":
			role = salesbrain.RoleBuyer
		case "human":
			role = salesbrain.RoleStaff
		}
		turns = append(turns, salesbrain.HistoryTurn{Role: role, Text: text})
	}
	return turns
}

// lfPrevBuyerMessageAt is when the buyer wrote before the current message (zero when this is the
// first one): the reminder policy only speaks up after a gap of at least half an hour.
func lfPrevBuyerMessageAt(history []dbMessage, currentText string) time.Time {
	past := lfPastHistory(history, currentText)
	for i := len(past) - 1; i >= 0; i-- {
		if past[i].Author == "contact" {
			return past[i].CreatedAt
		}
	}
	return time.Time{}
}

// lfBuyerTexts is the current message plus up to three earlier buyer messages: the numbers and
// amounts the buyer typed themselves may be echoed back in the reply.
func lfBuyerTexts(history []dbMessage, currentText string) []string {
	texts := []string{currentText}
	past := lfPastHistory(history, currentText)
	for i := len(past) - 1; i >= 0 && len(texts) < 4; i-- {
		if body := strings.TrimSpace(past[i].Body); past[i].Author == "contact" && body != "" {
			texts = append(texts, body)
		}
	}
	return texts
}

// ── open orders ──────────────────────────────────────────────────────────────

// lfOrderStage says what a saved draft still lacks, from the shipping data it holds: recipient
// (name or phone missing), address, or nothing (the order is complete and waits for payment).
func lfOrderStage(st *orderState) (salesbrain.Stage, string) {
	switch {
	case strings.TrimSpace(st.RecipientName) == "" || strings.TrimSpace(st.RecipientPhone) == "":
		return salesbrain.StageRecipient, salesbrain.StatusDraft
	case !st.ShippingComplete():
		return salesbrain.StageAddress, salesbrain.StatusDraft
	}
	return salesbrain.StagePayment, salesbrain.StatusAwaitingPayment
}

// lfOpenSnapshot maps one saved order to the reminder policy's view, or reports that it is not
// open. Open means: still a draft, not paid yet (a proof under review counts as paid), with items.
// Orders that are paid, processing, shipped or completed are only mentioned when the buyer asks.
func lfOpenSnapshot(o *persistedOrder) (salesbrain.OrderSnapshot, bool) {
	if o.Status != "draft" {
		return salesbrain.OrderSnapshot{}, false
	}
	switch strings.ToLower(strings.TrimSpace(o.PaymentStatus)) {
	case "", "unpaid", "rejected":
	default:
		return salesbrain.OrderSnapshot{}, false
	}
	st := orderStateFromPersistedDraft(o)
	cart := cartFromState(&st)
	if len(cart) == 0 {
		return salesbrain.OrderSnapshot{}, false
	}
	stage, status := lfOrderStage(&st)
	subtotal := lfRoundPrice(o.Subtotal)
	if subtotal <= 0 {
		subtotal = salesbrain.CartSubtotal(cart)
	}
	// The age of a draft is its creation (spec D4: "berumur ≤ 72 jam"); the order table read by the
	// legacy loaders carries no later timestamp.
	return salesbrain.OrderSnapshot{
		Ref: FormatOrderNumber(o.ID), Status: status, Stage: stage,
		Lines: cart, Subtotal: subtotal, UpdatedAt: o.CreatedAt,
	}, true
}

// mapOpenOrders returns the buyer's open orders for the prompt: newest first, at most three,
// none older than 72 hours (salesbrain.SelectOpenOrders applies the age and count rules).
func mapOpenOrders(orders []persistedOrder, now time.Time) []salesbrain.OrderSnapshot {
	var snaps []salesbrain.OrderSnapshot
	for i := range orders {
		if snap, ok := lfOpenSnapshot(&orders[i]); ok {
			snaps = append(snaps, snap)
		}
	}
	return salesbrain.SelectOpenOrders(snaps, now)
}

// lfStateFromOpenOrder rebuilds the checkout of a saved draft for "continue": the items, the
// shipping data given so far, the draft id (so the final save updates this draft) and the step
// that still has to be asked. ok is false when nothing is left to collect (the order waits for
// payment) or the draft has no items.
func lfStateFromOpenOrder(o *persistedOrder) (*orderState, bool) {
	st := orderStateFromPersistedDraft(o)
	if len(cartFromState(&st)) == 0 {
		return nil, false
	}
	stage, _ := lfOrderStage(&st)
	switch stage {
	case salesbrain.StageRecipient:
		st.Step = lfStepAskRecipient
	case salesbrain.StageAddress:
		st.Step = lfStepAskAddressFull
	default:
		return nil, false
	}
	return &st, true
}

// ── catalog, KB, profile ─────────────────────────────────────────────────────

// lfCatalogFacts numbers the catalog 1-based in load order, the numbering the model answers with
// (ItemOp.No). Stock is shown only for items whose stock is tracked. Variant data (size of the row,
// base name shared by sibling sizes, "needs a size or colour") is derived from the row with the
// legacy buyerflow rules, never from the model.
func lfCatalogFacts(catalog []dbCatalogItem) []salesbrain.CatalogFact {
	facts := make([]salesbrain.CatalogFact, len(catalog))
	for i := range catalog {
		it := catalog[i]
		stock, stockQty := "", 0.0
		if it.StockTracked {
			stock, stockQty = salesbrain.StockOut, it.StockAvailable
			if it.StockAvailable > 0 {
				stock = salesbrain.StockIn
			}
		}
		facts[i] = salesbrain.CatalogFact{
			No: i + 1, ID: it.ID, Name: it.Name, Price: lfRoundPrice(it.SellPrice), Unit: it.SellUnit,
			Stock: stock, Description: it.Description, Code: it.ExternalCode,
			BaseName: bf.ProductBaseName(it.Name), Size: bf.ExtractSizeFromProductName(it.Name),
			NeedsVariant: bf.CatalogItemNeedsVariant(&it), StockQty: stockQty,
		}
	}
	return facts
}

func lfKBFacts(kb []dbKBEntry) []salesbrain.KBFact {
	facts := make([]salesbrain.KBFact, len(kb))
	for i, e := range kb {
		facts[i] = salesbrain.KBFact{Question: e.Question, Answer: e.Answer, Category: strOrEmpty(e.Category)}
	}
	return facts
}

func lfProfileFact(p *dbBusinessProfile) salesbrain.ProfileFact {
	if p == nil {
		return salesbrain.ProfileFact{}
	}
	return salesbrain.ProfileFact{
		Name: p.BusinessName, Description: strOrEmpty(p.Description), Address: strOrEmpty(p.Address),
		OpeningHours: strOrEmpty(p.OpeningHours), DeliveryArea: strOrEmpty(p.DeliveryArea), Tone: strOrEmpty(p.Tone),
	}
}

// lfInCart reports whether a catalog row is already a line of the cart.
func lfInCart(c salesbrain.CatalogFact, cart []salesbrain.OrderLine) bool {
	for _, l := range cart {
		if (l.ID != "" && l.ID == c.ID) || (l.ID == "" && strings.EqualFold(l.Name, c.Name)) {
			return true
		}
	}
	return false
}

// topKCatalog narrows a big catalog to the k rows that best match the query (the message and the
// last turns), plus every cart item whatever its score, so the model can always name what the buyer
// already ordered. Rows keep their original numbers and order: the numbers are the contract with
// salesbrain.ApplyItemOps. With no match at all the first rows stand in (never an empty catalog).
// The name and the description both count for matching.
func topKCatalog(catalog []salesbrain.CatalogFact, query string, cart []salesbrain.OrderLine, k int) []salesbrain.CatalogFact {
	if k <= 0 || len(catalog) <= k {
		return catalog
	}
	q := bf.Tokenize(query)
	type scored struct {
		idx   int
		score float64
	}
	ranked := make([]scored, len(catalog))
	for i, c := range catalog {
		score := bf.OverlapScore(q, bf.Tokenize(c.Name))
		if c.Description != "" {
			score = max(score, bf.OverlapScore(q, bf.Tokenize(c.Name+" "+c.Description)))
		}
		ranked[i] = scored{idx: i, score: score}
	}
	sort.SliceStable(ranked, func(a, b int) bool { return ranked[a].score > ranked[b].score })

	keep := make(map[int]bool, k+len(cart))
	for _, r := range ranked[:k] {
		keep[r.idx] = true
	}
	for i, c := range catalog {
		if lfInCart(c, cart) {
			keep[i] = true
		}
	}
	// The other sizes of a kept product stay: they are the alternatives of a sold-out size and the
	// rows the model needs to pick another size.
	idxByNo := make(map[int]int, len(catalog))
	for i, c := range catalog {
		idxByNo[c.No] = i
	}
	for i := range catalog {
		if !keep[i] {
			continue
		}
		for _, sib := range salesbrain.Siblings(catalog, catalog[i]) {
			keep[idxByNo[sib.No]] = true
		}
	}
	out := make([]salesbrain.CatalogFact, 0, len(keep))
	for i, c := range catalog {
		if keep[i] {
			out = append(out, c)
		}
	}
	return out
}

// ── plan input ───────────────────────────────────────────────────────────────

// lfInputs is everything the shell loaded for one turn, ready for lfBuildPlanInput.
type lfInputs struct {
	Profile     *dbBusinessProfile
	Catalog     []dbCatalogItem
	KBAll       []dbKBEntry
	KBRetrieved []dbKBEntry // only read when TopK
	TopK        bool        // the catalog + FAQ do not fit the full-context budget
	History     []dbMessage
	LastBody    string // body of our newest outbound message
	State       *orderState
	Orders      []persistedOrder
	Remind      salesbrain.ReminderState
	Confused    int
	UserText    string
	Now         time.Time
	Lang        salesbrain.LanguageDecision
	LangLocked  bool
}

// lfTopKQueryTurns is how many earlier turns join the message when a big catalog is narrowed: a
// bare "how much is it?" is about the product named a moment ago.
const lfTopKQueryTurns = 3

// lfBuildPlanInput assembles the planTurn input. State never carries the buyer's stored phone or
// address: PendingField only says which field is still missing (spec D7).
func lfBuildPlanInput(in lfInputs) planInput {
	cart := cartFromState(in.State)
	pending := pendingFieldFromState(in.State)
	turns := historyTurnsFrom(in.History, in.UserText)

	facts := lfCatalogFacts(in.Catalog)
	kb := lfKBFacts(in.KBAll)
	if in.TopK {
		query := in.UserText
		for i := max(0, len(turns)-lfTopKQueryTurns); i < len(turns); i++ {
			query += " " + turns[i].Text
		}
		facts = topKCatalog(facts, query, cart, lfTopKRows)
		kb = lfKBFacts(kbForLLMPrompt(in.KBAll, in.KBRetrieved))
	}

	open := mapOpenOrders(in.Orders, in.Now)
	prev := lfPrevBuyerMessageAt(in.History, in.UserText)
	remindAllowed := salesbrain.ReminderAllowed(open, in.Remind, salesbrain.ReminderContext{
		Now: in.Now, PrevBuyerMessageAt: prev, FillingField: pending != "", Complaint: IsComplaintLike(in.UserText),
	})
	lastQuestion := ""
	if q := strings.TrimSpace(in.LastBody); strings.HasSuffix(q, "?") {
		lastQuestion = q
	}

	return planInput{
		PendingAfterVariant: pendingFieldAfterVariant(in.State),
		Lang:                in.Lang,
		LangLocked:          in.LangLocked,
		Profile:             lfProfileFact(in.Profile),
		Catalog:             facts,
		KB:                  kb,
		State: salesbrain.TurnState{
			Greeting:        greetingFor(in.Now),
			Now:             formatNowID(in.Now),
			ReturningBuyer:  !prev.IsZero(),
			OpenOrders:      open,
			RemindAllowed:   remindAllowed,
			Cart:            cart,
			PendingField:    pending,
			ResumeLine:      salesbrain.ResumeLineFor(pending, in.Lang.Lang),
			LastBotQuestion: lastQuestion,
			UpsellDone:      in.State != nil && in.State.UpsellShown,
			Misunderstood:   in.Confused,
		},
		History:    turns,
		Message:    in.UserText,
		BuyerTexts: lfBuyerTexts(in.History, in.UserText),
	}
}

// ── reminder state and counters ──────────────────────────────────────────────

// lfRemindRecord is the JSON kept in Redis at ai:openorder:remind:<tenant>:<conversation>.
type lfRemindRecord struct {
	LastAt time.Time      `json:"lastAt"`
	Count  map[string]int `json:"count"`
}

func lfEncodeRemind(rs salesbrain.ReminderState) []byte {
	raw, _ := json.Marshal(lfRemindRecord{LastAt: rs.LastAt, Count: rs.Count})
	return raw
}

// lfDecodeRemind reads the stored record; anything unreadable counts as "never reminded".
func lfDecodeRemind(raw []byte) salesbrain.ReminderState {
	var rec lfRemindRecord
	if len(raw) == 0 || json.Unmarshal(raw, &rec) != nil {
		return salesbrain.ReminderState{}
	}
	return salesbrain.ReminderState{LastAt: rec.LastAt, Count: rec.Count}
}

// lfNextRemindState records one more proactive reminder for ref at now.
func lfNextRemindState(rs salesbrain.ReminderState, ref string, now time.Time) salesbrain.ReminderState {
	count := make(map[string]int, len(rs.Count)+1)
	for k, v := range rs.Count {
		count[k] = v
	}
	count[ref]++
	return salesbrain.ReminderState{LastAt: now, Count: count}
}

// lfNextConfused is the "misunderstood in a row" counter: intent other counts, anything the model
// understood resets it.
func lfNextConfused(prev int, intent salesbrain.Intent) (next int, reset bool) {
	if intent == salesbrain.IntentOther {
		return prev + 1, false
	}
	return 0, true
}

// ── reply inspection ─────────────────────────────────────────────────────────

func lfReplyMentionsRef(reply, ref string) bool {
	return ref != "" && strings.Contains(strings.ToUpper(reply), strings.ToUpper(ref))
}

// lfReplyOffersUpsell reports whether the model's reply names a catalog product that is not in the
// cart: an offer of another product, which the order may carry at most once. Only full product
// names count, so an offer worded loosely is missed rather than guessed.
func lfReplyOffersUpsell(modelReply string, facts []salesbrain.CatalogFact, cart []salesbrain.OrderLine) bool {
	text := " " + lfNormText(modelReply) + " "
	for _, c := range facts {
		if lfInCart(c, cart) {
			continue
		}
		if name := lfNormText(c.Name); name != "" && strings.Contains(text, " "+name+" ") {
			return true
		}
	}
	return false
}

// lfViolationCodes lists the codes of a failed validation for the log. Details stay out: they
// quote the model's reply and may carry buyer data.
func lfViolationCodes(vs []salesbrain.Violation) string {
	codes := make([]string, len(vs))
	for i, v := range vs {
		codes[i] = v.Code
	}
	return strings.Join(codes, ",")
}

// lfAddressLine is the delivery address as one line, empty parts skipped.
func lfAddressLine(st *orderState) string {
	var parts []string
	for _, p := range []string{st.Street, st.Kelurahan, st.Kecamatan, st.City, st.Province, st.PostalCode} {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ", ")
}

// lfJoinProblems joins the invalid-data fragments of applyFieldAnswer with semicolons: no
// connector word, so the same code serves both languages (the wrapper sentence is a template).
func lfJoinProblems(problems []string) string { return strings.Join(problems, "; ") }
