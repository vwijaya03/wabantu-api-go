package pii

import "testing"

const testKey = "01234567890123456789012345678901"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	enc, err := Encrypt("Budi Santoso", testKey)
	if err != nil {
		t.Fatal(err)
	}
	if enc == "Budi Santoso" {
		t.Fatal("expected ciphertext")
	}
	plain, err := Decrypt(enc, testKey)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "Budi Santoso" {
		t.Fatalf("got %q", plain)
	}
}

func TestBlindIndexDeterministic(t *testing.T) {
	a := BlindIndex(NormalizePhone("0812-3456-7890"), testKey)
	b := BlindIndex(NormalizePhone("081234567890"), testKey)
	if a != b {
		t.Fatalf("phone indexes differ: %q vs %q", a, b)
	}
	if a == "" || len(a) != 64 {
		t.Fatalf("unexpected index: %q", a)
	}
}

func TestDecryptOrLegacy(t *testing.T) {
	enc, err := Encrypt("secret", testKey)
	if err != nil {
		t.Fatal(err)
	}
	v, err := DecryptOrLegacy(enc, "legacy", testKey)
	if err != nil || v != "secret" {
		t.Fatalf("got %q err=%v", v, err)
	}
	v, err = DecryptOrLegacy("", "legacy", testKey)
	if err != nil || v != "legacy" {
		t.Fatalf("got %q err=%v", v, err)
	}
}

func TestValidateKey(t *testing.T) {
	if err := ValidateKey("short"); err == nil {
		t.Fatal("expected error for short key")
	}
	if err := ValidateKey(testKey); err != nil {
		t.Fatal(err)
	}
}
