package admin

import (
	"strings"
	"testing"
)

func TestCheckTriageToken(t *testing.T) {
	good := strings.Repeat("z", 48)
	if checkTriageToken(good, "") == nil {
		t.Fatal("unset secret must fail closed")
	}
	if checkTriageToken("abc", "abc") == nil {
		t.Fatal("weak secret must fail closed")
	}
	if checkTriageToken("", good) == nil || checkTriageToken(good+"x", good) == nil {
		t.Fatal("empty/wrong token must be rejected")
	}
	if err := checkTriageToken(good, good); err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
}
