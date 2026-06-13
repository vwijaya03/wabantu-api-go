package ai

import (
	"fmt"
	"strings"
)

type orderOwnershipCase struct {
	ID       int
	Category string
	Name     string
	Pure     func() error
}

func allOrderOwnershipCases() []orderOwnershipCase {
	var out []orderOwnershipCase
	id := 1
	add := func(cat, name string, fn func() error) {
		out = append(out, orderOwnershipCase{ID: id, Category: cat, Name: name, Pure: fn})
		id++
	}

	const (
		localTestContact = "contact-local-test"
		ngiekContact     = "contact-ngiek"
		sharedConvo      = "7f3f02c4-6a0a-4e01-912f-0be70dde81ca"
		eaaOrderID       = "eaa94534-1758-4cbe-830c-b2ba16244b0c"
	)

	// ── 1. OrderAccessibleByContact (35) ──
	pairs := []struct {
		orderContact, reqContact, convo string
		want                            bool
	}{
		{ngiekContact, ngiekContact, sharedConvo, true},
		{ngiekContact, localTestContact, sharedConvo, false},
		{localTestContact, localTestContact, sharedConvo, true},
		{"", localTestContact, sharedConvo, true},
		{"", localTestContact, "other-convo", false},
		{ngiekContact, "", sharedConvo, false},
		{"", "", sharedConvo, true},
	}
	for i := 0; i < 35; i++ {
		p := pairs[i%len(pairs)]
		i, p := i, p
		add("contact_scope", fmt.Sprintf("pair_%d", i), func() error {
			o := &persistedOrder{ContactID: p.orderContact, ConversationID: sharedConvo}
			got := OrderAccessibleByContact(o, p.reqContact, p.convo)
			if got != p.want {
				return fmt.Errorf("OrderAccessibleByContact(%q,%q) = %v want %v", p.orderContact, p.reqContact, got, p.want)
			}
			return nil
		})
	}

	// ── 2. Phone guard (20) ──
	phoneCases := []struct {
		req, owner string
		want       bool
	}{
		{"6281292066606", "6281292066606", true},
		{"+6281292066606", "6281292066606", true},
		{"6281292066606", "+628888339455", false},
		{"628888339455", "628888339455", true},
		{"081292066606", "6281292066606", false},
		{"", "6281292066606", false},
		{"6281292066606", "", true},
	}
	for i := 0; i < 20; i++ {
		pc := phoneCases[i%len(phoneCases)]
		add("phone_guard", fmt.Sprintf("phone_%d", i), func() error {
			o := &persistedOrder{ID: eaaOrderID, ContactID: ngiekContact}
			got := OrderAccessibleByPhone(o, pc.req, pc.owner)
			if got != pc.want {
				return fmt.Errorf("OrderAccessibleByPhone(%q,%q) = %v want %v", pc.req, pc.owner, got, pc.want)
			}
			return nil
		})
	}

	// ── 3. Combined chat access (15) ──
	for i := 0; i < 15; i++ {
		i := i
		add("chat_access", fmt.Sprintf("combined_%d", i), func() error {
			o := &persistedOrder{
				ID:             eaaOrderID,
				ContactID:      ngiekContact,
				ConversationID: sharedConvo,
			}
			scope := orderAccessScope{ConversationID: sharedConvo, ContactID: localTestContact}
			if OrderChatAccessAllowed(o, scope, "6281292066606", "628888339455") {
				return fmt.Errorf("local test must not access ngiek order")
			}
			scope2 := orderAccessScope{ConversationID: sharedConvo, ContactID: ngiekContact}
			if !OrderChatAccessAllowed(o, scope2, "628888339455", "628888339455") {
				return fmt.Errorf("owner must access own order")
			}
			return nil
		})
	}

	// ── 4. Production incident WB-EAA94534 (10) ──
	prodMsgs := []string{
		"batalkan pesananku ya WB-EAA94534",
		"batalkan WB-EAA94534",
		"batal pesanan WB-EAA94534",
		"cancel order WB-EAA94534",
		"mau batalkan WB-EAA94534",
	}
	for _, msg := range prodMsgs {
		msg := msg
		add("production_eaa94534", truncOwnName(msg), func() error {
			ref := parseOrderRefFromMessage(msg)
			if ref != "WB-EAA94534" {
				return fmt.Errorf("ref = %q", ref)
			}
			o := &persistedOrder{
				ID: eaaOrderID, ContactID: ngiekContact, ConversationID: sharedConvo,
			}
			scope := orderAccessScope{ConversationID: sharedConvo, ContactID: localTestContact}
			if OrderAccessibleByContact(o, scope.ContactID, scope.ConversationID) {
				return fmt.Errorf("local test must not own WB-EAA94534 (ngiek order)")
			}
			return nil
		})
	}
	for i := 0; i < 5; i++ {
		add("production_eaa94534", fmt.Sprintf("deny_local_%d", i), func() error {
			o := &persistedOrder{ID: eaaOrderID, ContactID: ngiekContact, ConversationID: sharedConvo}
			if OrderAccessibleByContact(o, localTestContact, sharedConvo) {
				return fmt.Errorf("illegal cross-owner access")
			}
			return nil
		})
	}

	// ── 5. Adversarial ref guessing (10) ──
	for i, ref := range []string{"WB-EAA94534", "WB-947FC5C0", "WB-00000001", "wb-eaa94534", "WB-EB76635C"} {
		ref := ref
		add("adversarial_ref", fmt.Sprintf("guess_%d", i), func() error {
			o := &persistedOrder{ID: eaaOrderID, ContactID: ngiekContact, ConversationID: sharedConvo}
			scope := orderAccessScope{ConversationID: sharedConvo, ContactID: "attacker-" + ref}
			if OrderAccessibleByContact(o, scope.ContactID, scope.ConversationID) {
				return fmt.Errorf("attacker must not access order via ref knowledge")
			}
			return nil
		})
	}
	for i := 0; i < 5; i++ {
		add("adversarial_ref", fmt.Sprintf("pad_%d", i), func() error {
			if parseOrderRefFromMessage("batalkan WB-AAAAAA") != "WB-AAAAAA" {
				return fmt.Errorf("parse ref")
			}
			return nil
		})
	}

	// ── 6. Scope valid + legacy null contact (10) ──
	for i := 0; i < 10; i++ {
		i := i
		add("legacy_null_contact", fmt.Sprintf("legacy_%d", i), func() error {
			o := &persistedOrder{ID: "x", ContactID: "", ConversationID: sharedConvo}
			if !OrderAccessibleByContact(o, localTestContact, sharedConvo) {
				return fmt.Errorf("legacy order in same convo should be accessible")
			}
			if OrderAccessibleByContact(o, localTestContact, "other") {
				return fmt.Errorf("legacy order wrong convo must deny")
			}
			return nil
		})
	}

	if len(out) != 100 {
		panic(fmt.Sprintf("order ownership cases: want 100 got %d", len(out)))
	}
	return out
}

func truncOwnName(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, " ", "_"), "?", "")
	if len(s) > 36 {
		return s[:36]
	}
	return s
}
