package webhook

import (
	"os"
	"testing"
)

func TestInlineInboundAIEnvGate(t *testing.T) {
	t.Setenv("INLINE_INBOUND_AI", "1")
	if os.Getenv("INLINE_INBOUND_AI") != "1" {
		t.Fatal("expected INLINE_INBOUND_AI=1")
	}
}
