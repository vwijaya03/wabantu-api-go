package ai

import (
	"errors"
	"regexp"
	"strings"
	"time"

	bf "encore.app/wabantu/internal/buyerflow"
	"encore.app/wabantu/order"
)

// errPaymentProofAskOrder: the proof's target is ambiguous; the bot asks the buyer which order
// it is for instead of attaching it to an order whose earlier proof was rejected.
type errPaymentProofAskOrder struct{ Refs []string }

func (e *errPaymentProofAskOrder) Error() string { return "payment proof target ambiguous" }

func asAskOrder(err error) (*errPaymentProofAskOrder, bool) {
	var a *errPaymentProofAskOrder
	ok := errors.As(err, &a)
	return a, ok
}

// proofBurstWindow: contact texts this close before the proof image count as the same burst.
const proofBurstWindow = 2 * time.Minute

var newOrderWantRe = regexp.MustCompile(`(?i)\b(mau|ingin|pengen|pingin|minta)\s+(pesan|pesen|order|beli)\b`)
var paymentTalkRe = regexp.MustCompile(`(?i)\b(bukti|transfer|tf|bayar|pembayaran|lunas)\b`)

// isNewOrderIntentText: a buyer starting a NEW order (not "ini bukti transfer pesanan saya").
func isNewOrderIntentText(t string) bool {
	t = strings.TrimSpace(t)
	if t == "" || paymentTalkRe.MatchString(t) {
		return false
	}
	return bf.IsExplicitNewOrderStart(t) || newOrderWantRe.MatchString(t)
}

// burstHasNewOrderIntent: any contact text within proofBurstWindow before (or at) the image.
func burstHasNewOrderIntent(history []dbMessage, imageAt time.Time) bool {
	for _, m := range history {
		if m.Author != "contact" || m.Type != "text" {
			continue
		}
		if m.CreatedAt.After(imageAt) || imageAt.Sub(m.CreatedAt) > proofBurstWindow {
			continue
		}
		if isNewOrderIntentText(m.Body) {
			return true
		}
	}
	return false
}

// proofTargetSuperseded: never route a proof to an order whose earlier proof was rejected when a
// newer order exists or the same burst carries new-order intent. Other targets are unchanged.
func proofTargetSuperseded(target, latest *persistedOrder, burstNewOrder bool) bool {
	if target == nil || !strings.EqualFold(strings.TrimSpace(target.PaymentStatus), "rejected") {
		return false
	}
	if latest != nil && latest.ID != target.ID && latest.CreatedAt.After(target.CreatedAt) {
		return true
	}
	return burstNewOrder
}

func askOrderRefs(target, latest *persistedOrder) []string {
	var refs []string
	if target != nil {
		refs = append(refs, order.FormatOrderNumber(target.ID))
	}
	if latest != nil && (target == nil || latest.ID != target.ID) {
		refs = append(refs, order.FormatOrderNumber(latest.ID))
	}
	return refs
}

// paymentProofAskOrderReply is the Indonesian clarification sent to the buyer.
func paymentProofAskOrderReply(refs []string) string {
	msg := "Kak, bukti transfer ini untuk pesanan yang mana ya?"
	if len(refs) > 0 {
		msg += " (" + strings.Join(refs, " / ") + ")"
	}
	return msg + " Balas dengan nomor pesanannya lalu kirim ulang buktinya ya 🙏 Kalau mau pesan baru, selesaikan pesanannya dulu, nanti kami kirim ringkasan untuk dibayar."
}
