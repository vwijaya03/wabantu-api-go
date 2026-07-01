package admin

import "testing"

func TestCloudDDLScriptValidation(t *testing.T) {
	valid := []string{"tenant", "inventory", "all", ""}
	for _, s := range valid {
		if s == "" {
			continue
		}
		if s != "tenant" && s != "inventory" && s != "all" {
			t.Fatalf("unexpected valid script %q", s)
		}
	}
	if "bad" == "tenant" || "bad" == "inventory" || "bad" == "all" {
		t.Fatal("bad should not be valid")
	}
}

func TestDefaultGitHubRepo(t *testing.T) {
	if defaultGitHubRepo == "" {
		t.Fatal("default repo required")
	}
}
