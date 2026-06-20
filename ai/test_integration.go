package ai

import (
	"os"
	"testing"
)

func skipUnlessIntegrationTests(t *testing.T) {
	t.Helper()
	if os.Getenv("WABANTU_AI_INTEGRATION") != "1" {
		t.Skip("suite integration dilewati di Encore Cloud build; jalankan ./scripts/run-ai-integration-tests.sh")
	}
}
