package codesim

import "testing"

func TestAuthorizeSessionAccess_AccountOwner(t *testing.T) {
	row := &examSessionRow{AccountID: "acct-1", ClientToken: "tok-1"}
	if err := authorizeSessionAccess(row, "acct-1", ""); err != nil {
		t.Fatalf("expected account owner access, got %v", err)
	}
}

func TestAuthorizeSessionAccess_ClientToken(t *testing.T) {
	row := &examSessionRow{AccountID: "acct-1", ClientToken: "11111111-1111-1111-1111-111111111111"}
	token := "11111111-1111-1111-1111-111111111111"
	if err := authorizeSessionAccess(row, "", token); err != nil {
		t.Fatalf("expected client token access, got %v", err)
	}
}

func TestAuthorizeSessionAccess_DeniedWithoutCredentials(t *testing.T) {
	row := &examSessionRow{AccountID: "acct-1", ClientToken: "11111111-1111-1111-1111-111111111111"}
	if err := authorizeSessionAccess(row, "", ""); err == nil {
		t.Fatal("expected denied without account or client token")
	}
}

func TestAuthorizeSessionAccess_LegacyOpenSession(t *testing.T) {
	row := &examSessionRow{}
	if err := authorizeSessionAccess(row, "", ""); err != nil {
		t.Fatalf("expected legacy open session, got %v", err)
	}
}
