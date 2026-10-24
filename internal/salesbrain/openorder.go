package salesbrain

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Order statuses the reminder policy understands (the caller maps DB/Redis state onto these).
const (
	StatusDraft           = "draft"
	StatusAwaitingPayment = "awaiting_payment"
	// Statuses below exist only in the unfinished-orders list that answers the buyer ("do I have
	// pending orders?"); the reminder policy never selects them (see SelectRemindOrders).
	StatusAwaitingReview = "awaiting_review" // transfer proof submitted, shop is checking
	StatusProcessing     = "processing"      // paid/confirmed, the shop is working on it
)

// Stage is where an open order is stuck.
type Stage string

const (
	StageItems      Stage = "items"
	StageRecipient  Stage = "recipient"
	StageAddress    Stage = "address"
	StageConfirm    Stage = "confirm"
	StagePayment    Stage = "payment"
	StageReview     Stage = "review"     // transfer proof submitted, waiting for the shop to check it
	StageProcessing Stage = "processing" // confirmed/paid, the shop is processing it
)

// OrderLine is one product line of a cart or order.
type OrderLine struct {
	ID        string // catalog item id ("" for legacy rows without id)
	Name      string
	Qty       int
	UnitPrice int64
	// Variant of the line. Size is the size of the row itself (from its name) or the one the buyer
	// chose; Color is the buyer's. VariantMissing marks a line of a product that needs a size or
	// colour the buyer has not given yet: the order cannot move on until it is filled.
	Size           string
	Color          string
	VariantMissing bool
}

// OrderSnapshot is the caller's view of one cart/order of this buyer.
type OrderSnapshot struct {
	Ref       string // e.g. "WB-F4BFCE06"; empty for a cart that is not persisted yet
	Status    string // StatusDraft, StatusAwaitingPayment, or any other DB status (ignored)
	Stage     Stage
	Lines     []OrderLine
	Subtotal  int64
	UpdatedAt time.Time
	// Paid: the order is past the unpaid stage (proof submitted, confirmed, paid, processing).
	// Code never cancels it itself: a cancellation goes to the shop team.
	Paid bool
}

const (
	OpenOrderMaxAge      = 72 * time.Hour
	SessionGap           = 30 * time.Minute
	MaxRemindersPerOrder = 2
	maxOpenOrdersListed  = 3  // reminder cap
	MaxUnfinishedListed  = 10 // cap of the unfinished-orders list shown to the model
	maxLinesDescribed    = 3
)

// SelectRemindOrders is the policy of the PROACTIVE reminder only: drafts and unpaid orders updated
// within OpenOrderMaxAge, newest first, at most three. Paid/processing/shipped/completed/cancelled
// orders are never reminded. It must not be used to answer "do I have pending orders?": that
// answer comes from ListUnfinishedOrders, which has no age limit (incident 21 Oct 2026).
func SelectRemindOrders(snaps []OrderSnapshot, now time.Time) []OrderSnapshot {
	var out []OrderSnapshot
	for _, s := range snaps {
		if s.Status != StatusDraft && s.Status != StatusAwaitingPayment {
			continue
		}
		if now.Sub(s.UpdatedAt) > OpenOrderMaxAge {
			continue
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	if len(out) > maxOpenOrdersListed {
		out = out[:maxOpenOrdersListed]
	}
	return out
}

// ListUnfinishedOrders is the STATE that answers the buyer: every unfinished order of this buyer
// whatever its age (the caller already dropped cancelled/shipped/completed ones), newest first,
// at most MaxUnfinishedListed. more is how many were left out of the list.
func ListUnfinishedOrders(snaps []OrderSnapshot) (list []OrderSnapshot, more int) {
	list = append(list, snaps...)
	sort.SliceStable(list, func(i, j int) bool { return list[i].UpdatedAt.After(list[j].UpdatedAt) })
	if len(list) > MaxUnfinishedListed {
		more = len(list) - MaxUnfinishedListed
		list = list[:MaxUnfinishedListed]
	}
	return list, more
}

// ReminderState is persisted per conversation by the caller (Redis).
type ReminderState struct {
	LastAt time.Time
	Count  map[string]int // proactive reminders sent per order ref
}

// ReminderContext describes the current turn.
type ReminderContext struct {
	Now                time.Time
	PrevBuyerMessageAt time.Time // previous buyer message before this one; zero if none
	FillingField       bool      // a checkout field (recipient/address/confirm) is pending
	Complaint          bool
	HandoffActive      bool
}

// ReminderAllowed reports whether the assistant may proactively mention the newest open order:
// only at the start of a new session (previous buyer message ≥ SessionGap ago), once per
// session, at most MaxRemindersPerOrder times per order, and never while the buyer fills a
// checkout field, complains or waits for a human.
func ReminderAllowed(open []OrderSnapshot, rs ReminderState, rc ReminderContext) bool {
	if len(open) == 0 || rc.FillingField || rc.Complaint || rc.HandoffActive {
		return false
	}
	if !rc.PrevBuyerMessageAt.IsZero() && rc.Now.Sub(rc.PrevBuyerMessageAt) < SessionGap {
		return false
	}
	if !rs.LastAt.IsZero() && !rc.PrevBuyerMessageAt.IsZero() && rs.LastAt.After(rc.PrevBuyerMessageAt) {
		return false
	}
	if rs.Count[open[0].Ref] >= MaxRemindersPerOrder {
		return false
	}
	return true
}

// DescribeOrder renders "REF: items, total Rp…, stage" in the buyer language.
func DescribeOrder(o OrderSnapshot, lang Lang) string {
	ref := o.Ref
	if ref == "" {
		if lang == LangEN {
			ref = "Your current cart"
		} else {
			ref = "Keranjang yang tadi"
		}
	}
	items := describeLines(o.Lines, lang)
	if lang == LangEN {
		return fmt.Sprintf("%s: %s, total %s (shipping not included), %s", ref, items, FormatRupiah(o.Subtotal), stagePhrase(o.Stage, LangEN))
	}
	return fmt.Sprintf("%s: %s, total %s (belum termasuk ongkir), %s", ref, items, FormatRupiah(o.Subtotal), stagePhrase(o.Stage, LangID))
}

func describeLines(lines []OrderLine, lang Lang) string {
	if len(lines) == 0 {
		if lang == LangEN {
			return "items not recorded"
		}
		return "item tidak tercatat"
	}
	var parts []string
	for i, l := range lines {
		if i == maxLinesDescribed {
			rest := len(lines) - maxLinesDescribed
			if lang == LangEN {
				parts = append(parts, fmt.Sprintf("+%d more items", rest))
			} else {
				parts = append(parts, fmt.Sprintf("+%d item lainnya", rest))
			}
			break
		}
		parts = append(parts, fmt.Sprintf("%dx %s", l.Qty, LineLabel(l, lang)))
	}
	return strings.Join(parts, " + ")
}

func stagePhrase(s Stage, lang Lang) string {
	en := map[Stage]string{
		StageItems:      "still choosing items",
		StageRecipient:  "just waiting for the recipient's name and phone",
		StageAddress:    "just waiting for the delivery address",
		StageConfirm:    "waiting for your final confirmation",
		StagePayment:    "waiting for payment",
		StageReview:     "payment proof received, the shop is checking it",
		StageProcessing: "paid or confirmed, being processed by the shop",
	}
	id := map[Stage]string{
		StageItems:      "masih memilih produk",
		StageRecipient:  "tinggal nama dan nomor HP penerima",
		StageAddress:    "tinggal alamat pengiriman",
		StageConfirm:    "tinggal konfirmasi akhir",
		StagePayment:    "menunggu pembayaran",
		StageReview:     "bukti bayar sudah masuk, sedang dicek toko",
		StageProcessing: "sudah dibayar/dikonfirmasi, sedang diproses toko",
	}
	if lang == LangEN {
		if p, ok := en[s]; ok {
			return p
		}
		return "not finished yet"
	}
	if p, ok := id[s]; ok {
		return p
	}
	return "belum selesai"
}

// FormatRupiah formats an integer amount as "Rp1.750.000".
func FormatRupiah(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(s[i : i+3])
	}
	out := "Rp" + b.String()
	if neg {
		out = "-" + out
	}
	return out
}
