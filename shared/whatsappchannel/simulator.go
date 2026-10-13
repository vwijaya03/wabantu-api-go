package whatsappchannel

import (
	"crypto/rand"
	"encoding/hex"
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
