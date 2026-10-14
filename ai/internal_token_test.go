package ai

import (
	"strings"
	"testing"
)

func TestCheckInternalToken(t *testing.T) {
	good := strings.Repeat("a", 40)
	cases := []struct {
		name, tok, exp string
		ok             bool
	}{
		{"unset secret fails closed", "x", "", false},
		{"weak secret fails closed", "short", "short", false},
		{"empty token", "", good, false},
		{"wrong token", strings.Repeat("b", 40), good, false},
		{"prefix token", good[:39], good, false},
		{"match", good, good, true},
	}
	for _, c := range cases {
		if err := checkInternalToken(c.tok, c.exp); (err == nil) != c.ok {
			t.Errorf("%s: err=%v", c.name, err)
		}
	}
}
