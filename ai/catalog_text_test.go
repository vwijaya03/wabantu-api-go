package ai

import (
	"strings"
	"testing"
)

func TestCatalogTextSystemNotEmpty(t *testing.T) {
	if strings.TrimSpace(catalogTextSystem) == "" {
		t.Fatal("catalogTextSystem empty")
	}
	if !strings.Contains(catalogTextUserTpl, "%s") {
		t.Fatal("catalogTextUserTpl missing format placeholder")
	}
}
