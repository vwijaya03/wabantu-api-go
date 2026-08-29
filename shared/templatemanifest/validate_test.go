package templatemanifest

import (
	"encoding/json"
	"testing"
)

func TestValidateChatbotManifest(t *testing.T) {
	raw, err := json.Marshal(BuiltinChatbotPlayful())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(raw); err != nil {
		t.Fatalf("expected valid manifest: %v", err)
	}
}

func TestValidateRejectsBadKind(t *testing.T) {
	m := BuiltinChatbotPlayful()
	m.Kind = "unknown"
	if err := Validate(m); err == nil {
		t.Fatal("expected error for bad kind")
	}
}
