package tenantaccess

import (
	"testing"
	"time"
)

func TestFormatTextArray(t *testing.T) {
	got := formatTextArray([]string{"finance", "main"})
	if got != `{"finance","main"}` {
		t.Fatalf("unexpected array format: %s", got)
	}
	if formatTextArray(nil) != "{}" {
		t.Fatal("nil should be {}")
	}
}

func TestNormalizeModulesLimited(t *testing.T) {
	mods, err := normalizeModules(ScopeLimited, []string{"finance", "finance"})
	if err != nil {
		t.Fatal(err)
	}
	if len(mods) != 1 || mods[0] != "finance" {
		t.Fatalf("expected deduped finance, got %v", mods)
	}
	_, err = normalizeModules(ScopeLimited, []string{})
	if err == nil {
		t.Fatal("expected error for empty limited modules")
	}
	_, err = normalizeModules(ScopeLimited, []string{"bogus"})
	if err == nil {
		t.Fatal("expected error for unknown module")
	}
}

func TestExpiresAtFromDuration(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	exp := expiresAtFromDuration(Duration24h, now)
	if exp == nil {
		t.Fatal("expected expiry for 24h")
	}
	if exp.Sub(now) != 24*time.Hour {
		t.Fatalf("expected 24h delta, got %v", exp.Sub(now))
	}
	if expiresAtFromDuration(DurationPermanent, now) != nil {
		t.Fatal("permanent should have nil expiry")
	}
}

func TestDurationFromPreset(t *testing.T) {
	h, err := durationFromPreset("7d")
	if err != nil || h != Duration7d {
		t.Fatalf("7d: h=%d err=%v", h, err)
	}
	_, err = durationFromPreset("invalid")
	if err == nil {
		t.Fatal("expected invalid preset error")
	}
}

func TestIsGrantActive(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	if !isGrantActive(nil, now) {
		t.Fatal("nil expiry = permanent")
	}
	if isGrantActive(&past, now) {
		t.Fatal("past should be inactive")
	}
	if !isGrantActive(&future, now) {
		t.Fatal("future should be active")
	}
}
