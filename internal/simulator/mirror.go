package simulator

import (
	"context"
	"time"

	"encore.app/wabantu/internal/realtime"
)

// MirrorMsg is the message to dual-write (nil = state only).
type MirrorMsg struct {
	ID, Direction, Author, Type, Text, Status string
	CreatedAt                                 time.Time
}

// Mirror dual-writes a simulator message plus the conversation state (pricing tier, latest
// order with proofCheck) to Firestore. Best-effort: no-op when the mirror is disabled or the
// conversation is not on a simulator channel; Postgres stays the source of truth.
func Mirror(ctx context.Context, m *realtime.Mirror, q RowQuerier, qual Qualifier, tenantID, convoID string, msg *MirrorMsg) {
	if m == nil || m.W == nil || tenantID == "" || convoID == "" {
		return
	}
	if qual == nil {
		qual = func(t string) string { return `"` + t + `"` }
	}
	var ok bool
	if err := q(ctx, `SELECT EXISTS(SELECT 1 FROM `+qual("conversation")+` c JOIN `+qual("whatsapp_channel")+` w
		ON w.id = c.channel_id WHERE c.id = $1 AND w.provider = 'simulator')`, convoID).Scan(&ok); err != nil || !ok {
		return
	}
	if msg != nil {
		rm := realtime.Message{ID: msg.ID, Direction: msg.Direction, Author: msg.Author, Type: msg.Type,
			Text: msg.Text, Status: msg.Status, CreatedAt: msg.CreatedAt}
		if rm.Direction == "in" {
			rm.Direction = "inbound"
		} else if rm.Direction == "out" {
			rm.Direction = "outbound"
		}
		if rm.Type == "image" {
			rm.ImageURL = ImageURL(msg.ID)
		}
		m.Message(ctx, tenantID, convoID, rm)
	}
	pricing, order := LoadState(ctx, q, qual, convoID)
	orderID := ""
	var orderAny any
	if order != nil {
		orderID, orderAny = order.ID, order
	}
	var pricingAny any
	if pricing != nil {
		pricingAny = pricing
	}
	m.State(ctx, tenantID, convoID, pricingAny, orderAny, orderID)
}

// MirrorBroadcast mirrors a broadcast stored in a simulator conversation. tenantID comes from
// the broadcast job payload; jobs without it (queued before the field existed) skip the mirror.
func MirrorBroadcast(ctx context.Context, m *realtime.Mirror, q RowQuerier, qual Qualifier,
	tenantID, convoID, msgID, body string, at time.Time) bool {
	if tenantID == "" || msgID == "" {
		return false
	}
	Mirror(ctx, m, q, qual, tenantID, convoID,
		&MirrorMsg{ID: msgID, Direction: "out", Author: "human", Type: "text", Text: body, Status: "sent", CreatedAt: at})
	return true
}
