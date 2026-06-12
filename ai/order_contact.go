package ai

import (
	"context"
	"regexp"
	"strings"

	"encore.dev/rlog"

	"encore.app/wabantu/shared/pii"
)

var phoneLikeNameRe = regexp.MustCompile(`^\+?\d[\d\s\-()]{6,}$`)

// contactDisplayNameShouldUpdate is true when the contact has no usable name yet.
func contactDisplayNameShouldUpdate(currentName, phone, candidate string) bool {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return false
	}
	currentName = strings.TrimSpace(currentName)
	if currentName == "" || currentName == pii.Placeholder {
		return true
	}
	if phoneLikeNameRe.MatchString(currentName) {
		return true
	}
	phone = strings.TrimSpace(phone)
	if phone != "" && strings.EqualFold(currentName, phone) {
		return true
	}
	normPhone := strings.TrimPrefix(strings.TrimPrefix(phone, "+"), "62")
	if normPhone != "" && strings.TrimPrefix(strings.TrimPrefix(currentName, "+"), "0") == normPhone {
		return true
	}
	return false
}

// syncContactDisplayNameFromOrder copies the order recipient name onto the WhatsApp contact
// when the contact still has no real display name (common when Meta webhook sends phone only).
func syncContactDisplayNameFromOrder(ctx context.Context, q tenantQuerier, contactID, recipientName string) {
	recipientName = strings.TrimSpace(recipientName)
	contactID = strings.TrimSpace(contactID)
	if recipientName == "" || contactID == "" {
		return
	}

	c, err := loadContact(ctx, q, contactID)
	if err != nil || c == nil {
		if err != nil {
			rlog.Warn("AI order: load contact for name sync failed", "err", err, "contactId", contactID)
		}
		return
	}

	current := ""
	if c.DisplayName != nil {
		current = *c.DisplayName
	}
	if !contactDisplayNameShouldUpdate(current, c.PhoneNumber, recipientName) {
		return
	}

	key := strings.TrimSpace(secrets.DataEncryptionKey)
	if pii.ValidateKey(key) == nil {
		enc, encErr := pii.Encrypt(recipientName, key)
		if encErr != nil {
			rlog.Warn("AI order: encrypt contact name failed", "err", encErr, "contactId", contactID)
			return
		}
		idx := pii.BlindIndex(pii.NormalizeName(recipientName), key)
		_, err = q.ExecContext(ctx, `
			UPDATE contact
			SET display_name_enc = $1,
			    display_name_idx = NULLIF($2, ''),
			    display_name = $3,
			    updated_at = NOW()
			WHERE id = $4`,
			enc, idx, pii.Placeholder, contactID)
		if err != nil && isMissingPIIColumn(err) {
			_, err = q.ExecContext(ctx, `
				UPDATE contact SET display_name = $1, updated_at = NOW() WHERE id = $2`,
				recipientName, contactID)
		}
	} else {
		_, err = q.ExecContext(ctx, `
			UPDATE contact SET display_name = $1, updated_at = NOW() WHERE id = $2`,
			recipientName, contactID)
	}
	if err != nil {
		rlog.Warn("AI order: sync contact display name failed", "err", err, "contactId", contactID)
		return
	}
	rlog.Info("AI order: contact display name synced", "contactId", contactID)
}
