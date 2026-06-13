package order

import (
	"encoding/json"
	"strings"

	"encore.app/wabantu/shared/pii"
)

const contactJoinCols = `COALESCE(c.display_name_enc, ''), COALESCE(c.display_name, ''),
		COALESCE(c.phone_number_enc, ''), COALESCE(c.phone_number, '')`

func decryptContactField(enc, legacy string) string {
	key := strings.TrimSpace(secrets.DataEncryptionKey)
	v, err := pii.DecryptOrLegacy(enc, legacy, key)
	if err != nil {
		v = legacy
	}
	v = strings.TrimSpace(v)
	if v == "" || v == pii.Placeholder {
		return ""
	}
	return v
}

func applyContactBuyer(o *Order, nameEnc, nameLegacy, phoneEnc, phoneLegacy string) {
	if o == nil {
		return
	}
	o.ContactDisplayName = decryptContactField(nameEnc, nameLegacy)
	o.ContactPhone = decryptContactField(phoneEnc, phoneLegacy)
	if o.ContactDisplayName == "" && o.ShippingAddress != nil {
		o.ContactDisplayName = strings.TrimSpace(o.ShippingAddress.Name)
	}
}

func scanOrderWithContact(scan func(dest ...any) error) (Order, error) {
	var o Order
	var itemsRaw, addrRaw []byte
	var nameEnc, nameLegacy, phoneEnc, phoneLegacy string
	if err := scan(
		&o.ID, &o.ConversationID, &o.ContactID, &itemsRaw,
		&addrRaw, &o.Notes, &o.Status,
		&o.TrackingNumber, &o.Courier,
		&o.PaymentTransactionID, &o.Subtotal, &o.ShippingCost, &o.Total,
		&o.CreatedBy, &o.CreatedAt, &o.UpdatedAt,
		&nameEnc, &nameLegacy, &phoneEnc, &phoneLegacy,
	); err != nil {
		return o, err
	}
	o.OrderNumber = FormatOrderNumber(o.ID)
	if len(itemsRaw) > 0 {
		_ = json.Unmarshal(itemsRaw, &o.Items)
	}
	if o.Items == nil {
		o.Items = []OrderItem{}
	}
	if len(addrRaw) > 2 {
		var addr ShippingAddress
		if json.Unmarshal(addrRaw, &addr) == nil && (addr.Street != "" || strings.TrimSpace(addr.Name) != "") {
			o.ShippingAddress = &addr
		}
	}
	applyContactBuyer(&o, nameEnc, nameLegacy, phoneEnc, phoneLegacy)
	return o, nil
}

// BuyerLabel — nama tampilan untuk UI (contact WA, fallback penerima).
func BuyerLabel(o Order) string {
	if n := strings.TrimSpace(o.ContactDisplayName); n != "" {
		return n
	}
	if o.ShippingAddress != nil {
		if n := strings.TrimSpace(o.ShippingAddress.Name); n != "" {
			return n
		}
	}
	if p := strings.TrimSpace(o.ContactPhone); p != "" {
		return p
	}
	return "Tanpa contact"
}
