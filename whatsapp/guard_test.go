package whatsapp

import (
	"context"
	"errors"
	"testing"
)

func TestSendTextRefusesSimulatorRecipient(t *testing.T) {
	// No network: the guard must fire before credentials/HTTP are touched.
	_, err := SendText(context.Background(), "tok", "pnid", "+62000 1234567890", "hi")
	if !errors.Is(err, ErrSimulatorRecipient) {
		t.Fatalf("got %v", err)
	}
	if GuardRecipient("6281234567890") != nil {
		t.Fatal("real number blocked")
	}
}
