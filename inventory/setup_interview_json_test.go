package inventory

import (
	"encoding/json"
	"testing"
)

func TestInvSetupInterviewMessageResponseJSONFlat(t *testing.T) {
	resp := InvSetupInterviewMessageResponse{
		SessionID: "inv_setup_1",
		Phase:       "products",
		Messages: []invSetupMessage{
			{Role: "assistant", Content: "Halo"},
			{Role: "user", Content: "Jual frozen food"},
		},
		TokensUsed: 0,
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"sessionId", "messages", "tokensUsed"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("expected top-level %q in %s", key, string(raw))
		}
	}
	if _, ok := m["invSetupInterviewStartResponse"]; ok {
		t.Fatalf("unexpected nested struct key in %s", string(raw))
	}
}
