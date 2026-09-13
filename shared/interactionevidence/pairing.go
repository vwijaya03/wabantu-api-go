package interactionevidence

import (
	"strings"
	"time"
)

// Pairable is a channel-neutral message used to pair inbound→outbound.
type Pairable struct {
	ID             string
	Direction      string // in | out
	Body           string
	InboundReplyTo string
	Author         string // ai | system | staff | user
	CreatedAt      time.Time
}

// Pair is one inbound with its matching assistant reply.
type Pair struct {
	Inbound   Pairable
	Outbound  Pairable
	ByReplyTo bool
}

// PairTurns prefers metadata.inboundReplyTo, then the first later outbound.
// Staff/system-only outbound does not steal pairing from the next AI reply.
func PairTurns(msgs []Pairable) []Pair {
	outByReply := map[string]Pairable{}
	usedOut := map[string]struct{}{}
	for _, m := range msgs {
		if !strings.EqualFold(m.Direction, "out") {
			continue
		}
		if isStaff(m.Author) {
			continue
		}
		ref := strings.TrimSpace(m.InboundReplyTo)
		if ref == "" {
			continue
		}
		if _, exists := outByReply[ref]; !exists {
			outByReply[ref] = m
		}
	}

	var pairs []Pair
	for i, m := range msgs {
		if !strings.EqualFold(m.Direction, "in") {
			continue
		}
		if out, ok := outByReply[m.ID]; ok {
			usedOut[out.ID] = struct{}{}
			pairs = append(pairs, Pair{Inbound: m, Outbound: out, ByReplyTo: true})
			continue
		}
		if out, ok := firstOutboundAfter(msgs, i, usedOut); ok {
			usedOut[out.ID] = struct{}{}
			pairs = append(pairs, Pair{Inbound: m, Outbound: out, ByReplyTo: false})
		}
	}
	return pairs
}

func firstOutboundAfter(msgs []Pairable, inboundIdx int, used map[string]struct{}) (Pairable, bool) {
	for j := inboundIdx + 1; j < len(msgs); j++ {
		m := msgs[j]
		if !strings.EqualFold(m.Direction, "out") {
			continue
		}
		if isStaff(m.Author) {
			continue
		}
		if _, taken := used[m.ID]; taken {
			continue
		}
		return m, true
	}
	return Pairable{}, false
}

func isStaff(author string) bool {
	a := strings.ToLower(strings.TrimSpace(author))
	return a == "staff" || a == "agent" || a == "human"
}
