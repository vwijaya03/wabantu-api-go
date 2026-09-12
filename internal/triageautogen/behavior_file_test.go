package triageautogen

import (
	"strings"
	"testing"
)

func TestBuildBehaviorTestFileEscapesCustomerText(t *testing.T) {
	src := BuildBehaviorTestFile(
		"41191bb5-a79f-4820-bf61-298c8757e85d",
		`saya mau "durian" 1`+"\npackage main",
		"order_flow",
		"durian",
		1,
		[]string{"oatlife"},
		nil,
		[]string{"White Coffee"},
	)
	if strings.Count(src, "package buyerflow") != 1 {
		t.Fatal("customer text must not inject another package clause")
	}
	if !strings.Contains(src, `saya mau \"durian\"`) {
		t.Fatalf("expected quoted customer text, got:\n%s", src)
	}
	if !strings.Contains(src, "41191bb5-a79f-4820-bf61-298c8757e85d") {
		t.Fatal("full job UUID required in file")
	}
	if AutoGenRelPath("41191bb5-a79f-4820-bf61-298c8757e85d") == BehaviorRelPath("41191bb5-a79f-4820-bf61-298c8757e85d") {
		t.Fatal("behavior path must differ from 8-char routing autogen")
	}
}
