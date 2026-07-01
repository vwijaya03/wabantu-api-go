package ai

import (
	"fmt"
	"strings"

	"encore.app/wabantu/whatsapp"
)

// orderAccessScope — identitas pembeli via chat (conversation + contact WhatsApp).
type orderAccessScope struct {
	ConversationID string
	ContactID      string
}

func (s orderAccessScope) valid() bool {
	return strings.TrimSpace(s.ConversationID) != "" && strings.TrimSpace(s.ContactID) != ""
}

// sqlOrderOwnerFilter — hanya order milik contact ini, atau legacy tanpa contact_id di conversation yang sama.
// paramConvo dan paramContact adalah indeks placeholder $N di query pemanggil.
func sqlOrderOwnerFilter(paramConvo, paramContact int) string {
	return fmt.Sprintf(` AND (
		(contact_id IS NOT NULL AND contact_id = $%d::uuid)
		OR (contact_id IS NULL AND conversation_id = $%d::uuid)
	)`, paramContact, paramConvo)
}

// OrderAccessibleByContact — pure check: apakah order boleh diakses dari chat contact ini.
func OrderAccessibleByContact(o *persistedOrder, contactID, conversationID string) bool {
	if o == nil {
		return false
	}
	orderContact := strings.TrimSpace(o.ContactID)
	reqContact := strings.TrimSpace(contactID)
	if orderContact != "" {
		return reqContact != "" && orderContact == reqContact
	}
	return strings.TrimSpace(o.ConversationID) == strings.TrimSpace(conversationID)
}

// OrderAccessibleByPhone — lapisan tambahan: nomor WA pengirim harus cocok dengan contact order (jika ada).
func OrderAccessibleByPhone(o *persistedOrder, requesterPhone string, orderContactPhone string) bool {
	if o == nil {
		return false
	}
	req := whatsapp.NormalizeRecipient(requesterPhone)
	owner := whatsapp.NormalizeRecipient(orderContactPhone)
	if req == "" {
		return false
	}
	if owner == "" {
		return true
	}
	return req == owner
}

// OrderChatAccessAllowed — gabungan contact_id + optional phone guard.
func OrderChatAccessAllowed(o *persistedOrder, scope orderAccessScope, requesterPhone, orderContactPhone string) bool {
	if !OrderAccessibleByContact(o, scope.ContactID, scope.ConversationID) {
		return false
	}
	if strings.TrimSpace(requesterPhone) == "" {
		return true
	}
	return OrderAccessibleByPhone(o, requesterPhone, orderContactPhone)
}

func orderAccessDeniedReply() string {
	return "Pesanan tersebut bukan terdaftar atas nomor WhatsApp ini ya kak. Hanya pemilik pesanan yang bisa cek atau batalkan lewat chat ini."
}

const persistedOrderSelectCols = `id::text,
		COALESCE(contact_id::text, ''),
		COALESCE(conversation_id::text, ''),
		status, items, shipping_address, subtotal, total, created_at,
		COALESCE(payment_status, 'unpaid'),
		COALESCE(payment_proof_meta, '{}')`

func scanPersistedOrderRow(scan func(dest ...any) error) (*persistedOrder, error) {
	var o persistedOrder
	err := scan(
		&o.ID, &o.ContactID, &o.ConversationID,
		&o.Status, &o.ItemsJSON, &o.ShippingJSON,
		&o.Subtotal, &o.Total, &o.CreatedAt,
		&o.PaymentStatus, &o.PaymentProofMetaJSON,
	)
	if err != nil {
		return nil, err
	}
	return &o, nil
}
