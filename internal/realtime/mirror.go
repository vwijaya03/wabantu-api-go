package realtime

import (
	"context"
	"sync"
	"time"
)

// Document paths (see docs/API_AI_CONFIG.md):
//
//	tenants/{tenantId}/simConversations/{conversationId}
//	tenants/{tenantId}/simConversations/{conversationId}/messages/{messageId}
//	tenants/{tenantId}/orders/{orderId}
func ConversationPath(tenantID, convoID string) string {
	return "tenants/" + tenantID + "/simConversations/" + convoID
}
func MessagePath(tenantID, convoID, msgID string) string {
	return ConversationPath(tenantID, convoID) + "/messages/" + msgID
}
func OrderPath(tenantID, orderID string) string { return "tenants/" + tenantID + "/orders/" + orderID }

// Message is the mirrored simulator message (same shape as GET /simulator/messages items).
type Message struct {
	ID        string
	Direction string // inbound | outbound
	Author    string
	Type      string
	Text      string
	Status    string
	ImageURL  string
	CreatedAt time.Time
}

// Mirror dual-writes best-effort: a nil Writer is a no-op, errors are logged, never returned.
type Mirror struct {
	W       Writer
	Timeout time.Duration
	Logf    func(format string, args ...any)
}

func (m *Mirror) enabled() bool { return m != nil && m.W != nil }

func (m *Mirror) set(ctx context.Context, path string, data map[string]any) {
	if !m.enabled() {
		return
	}
	to := m.Timeout
	if to <= 0 {
		to = 3 * time.Second
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), to)
	defer cancel()
	if err := m.W.Set(cctx, path, data); err != nil && m.Logf != nil {
		m.Logf("realtime: firestore write failed path=%s err=%v", path, err)
	}
}

// Message mirrors one simulator message and bumps the conversation doc.
func (m *Mirror) Message(ctx context.Context, tenantID, convoID string, msg Message) {
	if !m.enabled() || tenantID == "" || convoID == "" || msg.ID == "" {
		return
	}
	data := map[string]any{
		"id": msg.ID, "direction": msg.Direction, "author": msg.Author, "type": msg.Type,
		"text": msg.Text, "status": msg.Status, "createdAt": msg.CreatedAt,
	}
	if msg.ImageURL != "" {
		data["imageUrl"] = msg.ImageURL
	} else {
		data["imageUrl"] = nil
	}
	m.set(ctx, MessagePath(tenantID, convoID, msg.ID), data)
	m.set(ctx, ConversationPath(tenantID, convoID), map[string]any{
		"conversationId": convoID, "lastMessageAt": msg.CreatedAt, "updatedAt": time.Now(),
	})
}

// State mirrors conversation-level pricing/order (any of them may be nil) and the order doc.
func (m *Mirror) State(ctx context.Context, tenantID, convoID string, pricing, order any, orderID string) {
	if !m.enabled() || tenantID == "" {
		return
	}
	if convoID != "" {
		m.set(ctx, ConversationPath(tenantID, convoID), map[string]any{
			"conversationId": convoID, "pricing": pricing, "order": order, "updatedAt": time.Now(),
		})
	}
	if orderID != "" && order != nil {
		m.set(ctx, OrderPath(tenantID, orderID), map[string]any{
			"conversationId": convoID, "order": order, "updatedAt": time.Now(),
		})
	}
}

var (
	defOnce sync.Once
	def     *Mirror
)

// Default is the process mirror from ENV (no-op when unconfigured).
func Default(logf func(string, ...any)) *Mirror {
	defOnce.Do(func() {
		def = &Mirror{Logf: logf, Timeout: 2 * time.Second}
		if cfg, err := EnvConfig(); err == nil {
			def.W = NewRESTWriter(cfg)
		}
	})
	return def
}
