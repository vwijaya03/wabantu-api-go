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
)

// Stage is where an open order is stuck.
type Stage string

const (
	StageItems     Stage = "items"
	StageRecipient Stage = "recipient"
	StageAddress   Stage = "address"
	StageConfirm   Stage = "confirm"
	StagePayment   Stage = "payment"
)

// OrderLine is one product line of a cart or order.
type OrderLine struct {
	Name      string
	Qty       int
	UnitPrice int64
}

// OrderSnapshot is the caller's view of one cart/order of this buyer.
type OrderSnapshot struct {
	Ref       string // e.g. "WB-F4BFCE06"; empty for a cart that is not persisted yet
	Status    string // StatusDraft, StatusAwaitingPayment, or any other DB status (ignored)
	Stage     Stage
	Lines     []OrderLine
	Subtotal  int64
	UpdatedAt time.Time
}

const (
	OpenOrderMaxAge      = 72 * time.Hour
	SessionGap           = 30 * time.Minute
	MaxRemindersPerOrder = 2
	maxOpenOrdersListed  = 3
	maxLinesDescribed    = 3
)

// SelectOpenOrders keeps drafts and unpaid orders updated within OpenOrderMaxAge, newest first,
// at most three. Paid/processing/shipped/completed/cancelled orders are never "open".
func SelectOpenOrders(snaps []OrderSnapshot, now time.Time) []OrderSnapshot {
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
		parts = append(parts, fmt.Sprintf("%dx %s", l.Qty, l.Name))
	}
	return strings.Join(parts, " + ")
}

func stagePhrase(s Stage, lang Lang) string {
	en := map[Stage]string{
		StageItems:     "still choosing items",
		StageRecipient: "just waiting for the recipient's name and phone",
		StageAddress:   "just waiting for the delivery address",
		StageConfirm:   "waiting for your final confirmation",
		StagePayment:   "waiting for payment",
	}
	id := map[Stage]string{
		StageItems:     "masih memilih produk",
		StageRecipient: "tinggal nama dan nomor HP penerima",
		StageAddress:   "tinggal alamat pengiriman",
		StageConfirm:   "tinggal konfirmasi akhir",
		StagePayment:   "menunggu pembayaran",
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
