package triageincident

import "testing"

func TestFingerprintIsTenantScoped(t *testing.T) {
	a := Compute(FingerprintInput{TenantID: "t1", Channel: "whatsapp", Lane: "buyerflow", FailureKind: "wrong_sku", CatalogIDs: []string{"durian"}})
	b := Compute(FingerprintInput{TenantID: "t2", Channel: "whatsapp", Lane: "buyerflow", FailureKind: "wrong_sku", CatalogIDs: []string{"durian"}})
	if a == b {
		t.Fatal("fingerprint must not collide across tenants")
	}
	c := Compute(FingerprintInput{TenantID: "t1", Channel: "web_chat", Lane: "buyerflow", FailureKind: "wrong_sku", CatalogIDs: []string{"durian"}})
	if a == c {
		t.Fatal("channel is part of per-incident fingerprint")
	}
	if CrossChannelKey(FingerprintInput{TenantID: "t1", Channel: "whatsapp", Lane: "buyerflow", FailureKind: "wrong_sku", CatalogIDs: []string{"durian"}}) !=
		CrossChannelKey(FingerprintInput{TenantID: "t1", Channel: "web_chat", Lane: "buyerflow", FailureKind: "wrong_sku", CatalogIDs: []string{"durian"}}) {
		t.Fatal("cross-channel key should match same root cause")
	}
}
