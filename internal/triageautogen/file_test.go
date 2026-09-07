package triageautogen

import (
	"strings"
	"testing"
)

func TestAutoGenJobSuffix(t *testing.T) {
	got := AutoGenJobSuffix("0aa8d9f4-62b2-43ab-ae3d-0c0fbd61bb1d")
	if got != "0aa8d9f4" {
		t.Fatalf("suffix = %q want 0aa8d9f4", got)
	}
	if AutoGenJobSuffix("") != "unknown" {
		t.Fatal("empty job id should fall back to unknown")
	}
}

func TestAutoGenRelPath(t *testing.T) {
	got := AutoGenRelPath("0aa8d9f4-62b2-43ab-ae3d-0c0fbd61bb1d")
	want := "internal/buyerflow/regression_autogen_0aa8d9f4_test.go"
	if got != want {
		t.Fatalf("path = %q want %q", got, want)
	}
}

func TestBuildAutoGenTestFile_uniqueSymbols(t *testing.T) {
	generated := `package buyerflow

const triageAutoGenSnapshotJSON = "{}"

func conversationRegressionAutoGenCases() []regressionCase {
	return nil
}
`
	a := BuildAutoGenTestFile("0aa8d9f4-62b2-43ab-ae3d-0c0fbd61bb1d", generated)
	b := BuildAutoGenTestFile("6c43985f-58e1-4b8f-ad51-4276b69d4c49", generated)
	if strings.Contains(a, "const triageAutoGenSnapshotJSON =") {
		t.Fatal("job A must suffix snapshot const")
	}
	if !strings.Contains(a, "triageAutoGenSnapshotJSON_0aa8d9f4") {
		t.Fatalf("missing unique const in A: %s", a)
	}
	if !strings.Contains(b, "triageAutoGenSnapshotJSON_6c43985f") {
		t.Fatalf("missing unique const in B: %s", b)
	}
	if !strings.Contains(a, "func TestRegressionAutoGen_0aa8d9f4") {
		t.Fatal("missing unique test func in A")
	}
	if strings.Contains(a, "func TestRegressionAutoGen(") {
		t.Fatal("generic TestRegressionAutoGen must not remain")
	}
}

func TestRewriteAutoGenSymbols_idempotent(t *testing.T) {
	src := BuildAutoGenTestFile("0aa8d9f4-62b2-43ab-ae3d-0c0fbd61bb1d", "const triageAutoGenSnapshotJSON = \"\"\n")
	again := RewriteAutoGenSymbols(src, "0aa8d9f4")
	if strings.Count(again, "triageAutoGenSnapshotJSON_0aa8d9f4_0aa8d9f4") != 0 {
		t.Fatal("rewrite must not double-suffix")
	}
}
