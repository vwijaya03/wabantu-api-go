package whatsappchannel

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// SimulatorProvider marks a tenant's judge/demo simulator channel. Outbound messages on such a
// channel are persisted only and are never sent to Meta.
const SimulatorProvider = "simulator"

// IsSimulator reports whether a channel provider is the simulator.
func IsSimulator(provider string) bool { return provider == SimulatorProvider }

// SimulatorExternalID returns a unique synthetic external id for simulator messages.
func SimulatorExternalID(prefix string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "sim-" + prefix + "-" + hex.EncodeToString(b)
}

// SimulatorPhonePrefix prefixes every synthetic simulator buyer phone ("62000" + 10 digits).
// 62-000 is not an allocated Indonesian range, so it can never be a real WhatsApp user.
const SimulatorPhonePrefix = "62000"

// IsSimulatorPhone reports whether a (digits-only or +-prefixed) phone is a simulator buyer.
func IsSimulatorPhone(phone string) bool {
	p := strings.TrimPrefix(strings.TrimSpace(phone), "+")
	if len(p) != len(SimulatorPhonePrefix)+10 || !strings.HasPrefix(p, SimulatorPhonePrefix) {
		return false
	}
	for _, r := range p {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ListableChannelSQL is a WHERE fragment hiding simulator channels from regular channel lists.
const ListableChannelSQL = "provider <> '" + SimulatorProvider + "'"
