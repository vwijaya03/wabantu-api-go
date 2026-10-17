package ai

import (
	"context"
	"errors"
	"testing"

	"encore.app/wabantu/shared/whatsappchannel"
)

// WhatsApp rejects an empty body, and an empty AI reply is always a bug upstream (a localized KB
// answer that came out empty did exactly that). sendAiMessage must refuse before it touches the
// channel, the DB or Meta.
func TestSendEmptyBodyIsRefused(t *testing.T) {
	var metaCalls, simCalls int
	oldMeta, oldSim := metaSendText, simulatorOutbound
	t.Cleanup(func() { metaSendText, simulatorOutbound = oldMeta, oldSim })
	metaSendText = func(ctx context.Context, tok, pid, to, body string) (string, error) {
		metaCalls++
		return "", errors.New("spy: meta send must not be reached")
	}
	simulatorOutbound = func(_ *AutoReplyService, _ context.Context, _ tenantScopedQuerier, _ string, _ *dbConversation, text, _, _ string, _ AiReplyMeta) error {
		simCalls++
		return nil
	}
	s := &AutoReplyService{}
	convo := &dbConversation{ID: "c"}
	metaCh := &dbChannel{Provider: "meta_cloud"} // not even a usable channel: the guard comes first
	simCh := &dbChannel{Provider: whatsappchannel.SimulatorProvider, Status: "connected"}

	for _, text := range []string{"", "   ", "\n\t "} {
		for _, ch := range []*dbChannel{metaCh, simCh} {
			err := s.sendAiMessage(context.Background(), nil, "t", convo, ch, &dbContact{}, text, "ai", "in-1", AiReplyMeta{})
			if !errors.Is(err, errEmptyReply) {
				t.Fatalf("text %q on %s: err = %v, want errEmptyReply", text, ch.Provider, err)
			}
		}
	}
	if metaCalls != 0 || simCalls != 0 {
		t.Fatalf("an empty body reached a sender: meta=%d simulator=%d", metaCalls, simCalls)
	}

	// A real reply is not affected by the guard.
	if err := s.sendAiMessage(context.Background(), fakeTS{}, "t", convo, simCh, &dbContact{ID: "k"}, "halo kak", "ai", "in-2", AiReplyMeta{}); err != nil {
		t.Fatalf("non-empty reply refused: %v", err)
	}
	if simCalls != 1 {
		t.Fatalf("non-empty reply must be delivered once, simulator calls = %d", simCalls)
	}
}
