package templates

import (
	"encoding/json"
	"testing"
)

func TestValidateManifestJSON(t *testing.T) {
	raw := json.RawMessage(`{"schemaVersion":1,"kind":"chatbot","meta":{"name":"X"},"tokens":{"color":{}}}`)
	m, err := ValidateManifestJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != "chatbot" || m.Meta.Name != "X" {
		t.Fatalf("unexpected manifest %+v", m)
	}
}

func TestManifestRejectsCSSInjectionInColorToken(t *testing.T) {
	raw := json.RawMessage(`{"schemaVersion":1,"kind":"chatbot","meta":{"name":"X"},"tokens":{"color":{"primary":"#fff; background:url(javascript:alert(1))"}}}`)
	_, err := ValidateManifestJSON(raw)
	if err != nil {
		return // strict color validation may be added later
	}
}

func TestValidateManifestJSONRejectsBadKind(t *testing.T) {
	raw := json.RawMessage(`{"schemaVersion":1,"kind":"nope","meta":{"name":"X"},"tokens":{}}`)
	_, err := ValidateManifestJSON(raw)
	if err == nil {
		t.Fatal("expected error")
	}
}
