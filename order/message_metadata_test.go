package order

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestOrderPaymentNotifyMetadataJSON(t *testing.T) {
	raw := orderPaymentNotifyMetadataJSON()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	wantKeys := []string{"llmUsed", "path", "reason"}
	gotKeys := make([]string, 0, len(m))
	for k := range m {
		gotKeys = append(gotKeys, k)
	}
	slices.Sort(gotKeys)
	if !slices.Equal(gotKeys, wantKeys) {
		t.Fatalf("keys = %v, want exactly %v", gotKeys, wantKeys)
	}

	var meta systemMessageMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("unmarshal struct: %v", err)
	}
	if meta.Path != pathOrderPaymentNotify {
		t.Fatalf("path = %q", meta.Path)
	}
	if meta.Reason != reasonNonQuestion {
		t.Fatalf("reason = %q", meta.Reason)
	}
	if meta.LLMUsed {
		t.Fatal("llmUsed should be false")
	}

	forbidden := []string{
		"body", "text", "phone", "phoneNumber", "amount", "proofUrl", "proofURL",
		"buyer", "customer", "bank", "accountNumber", "fileHash",
	}
	for _, k := range forbidden {
		if _, ok := m[k]; ok {
			t.Fatalf("forbidden PII/sensitive key %q present in metadata", k)
		}
	}
}
