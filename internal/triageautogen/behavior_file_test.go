package triageautogen

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestBuildBehaviorTestFileEscapesCustomerText(t *testing.T) {
	src := BuildBehaviorTestFile(
		"41191bb5-a79f-4820-bf61-298c8757e85d",
		`saya mau "durian" 1`+"\npackage main",
		"order_flow",
		[]CartInclude{{Name: "durian", Qty: 1}},
		[]string{"oatlife"},
		nil,
		[]string{"White Coffee"},
		false,
	)
	if strings.Count(src, "package buyerflow_test") != 1 {
		t.Fatal("generated test must use external test package buyerflow_test")
	}
	if strings.Contains(src, "package buyerflow\n") {
		t.Fatal("package buyerflow would import-cycle with triageassert")
	}
	if !strings.Contains(src, "buyerflow.NewOmahSimulator()") {
		t.Fatal("external tests must call buyerflow.NewOmahSimulator")
	}
	if !strings.Contains(src, `saya mau \"durian\"`) {
		t.Fatalf("expected quoted customer text, got:\n%s", src)
	}
	if !strings.Contains(src, "41191bb5-a79f-4820-bf61-298c8757e85d") {
		t.Fatal("full job UUID required in file")
	}
	if !strings.Contains(BehaviorRelPath("41191bb5-a79f-4820-bf61-298c8757e85d"), "regression_behavior_41191bb5-a79f-4820-bf61-298c8757e85d") {
		t.Fatal("behavior path must use full job UUID")
	}
	if !strings.Contains(src, "func TestBehavior_41191bb5_a79f_4820_bf61_298c8757e85d(") {
		t.Fatalf("GHA runner must match TestBehavior_, got:\n%s", src)
	}
	if strings.Contains(src, "func TestRegressionAutoGen") {
		t.Fatal("behavior tests must not use TestRegressionAutoGen — that filter would skip them")
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "regression_behavior_x_test.go", src, 0)
	if err != nil {
		t.Fatalf("generated test must parse: %v\n%s", err, src)
	}
	if f.Name.Name != "buyerflow_test" {
		t.Fatalf("package %s", f.Name.Name)
	}
}

func TestBuildBehaviorTestFileHydratesDraftFixture(t *testing.T) {
	src := BuildBehaviorTestFile(
		"e26179a5-cfe8-4109-a5af-dfeb19fc7dcb",
		"cadburry nya tolong dibatalkan, lalu musang king durian nya mau nambah 1 ya",
		"order_flow",
		[]CartInclude{{Name: "durian", Qty: 2}},
		[]string{"cadbury"},
		nil,
		nil,
		true,
	)
	if !strings.Contains(src, "buyerflow.NewWB239488Simulator()") {
		t.Fatalf("cart mutation tests must hydrate WB-239488D0, got:\n%s", src)
	}
}
