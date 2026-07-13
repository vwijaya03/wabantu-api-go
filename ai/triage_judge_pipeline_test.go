package ai

import (
	"strings"
	"testing"
)

func TestRelevantCatalogForTurn_mentionsOnly(t *testing.T) {
	catalog := omahCatalog()
	turn := AITriageTurn{
		UserText:  "WB-372AF9ED status pesanan e apa ?",
		ReplyText: "Pesanan WB-372AF9ED: Abon Sapi 500G x 3, total Rp105000",
	}
	compact := relevantCatalogForTurn(turn, catalog)
	if len(compact) == 0 {
		t.Fatal("expected at least one catalog item")
	}
	found := false
	for _, it := range compact {
		if strings.Contains(it.Name, "Abon Sapi") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected Abon Sapi in compact catalog, got %+v", compact)
	}
	if len(compact) >= len(catalog) {
		t.Fatalf("compact catalog should be smaller than full catalog: %d vs %d", len(compact), len(catalog))
	}
}

func TestRelevantCatalogForTurn_fallbackWhenNoMatch(t *testing.T) {
	catalog := omahCatalog()
	turn := AITriageTurn{UserText: "halo kak", ReplyText: "Halo! Ada yang bisa dibantu?"}
	compact := relevantCatalogForTurn(turn, catalog)
	if len(compact) == 0 {
		t.Fatal("expected fallback catalog slice")
	}
	if len(compact) > triageCompactCatalogFallback {
		t.Fatalf("fallback len = %d want <= %d", len(compact), triageCompactCatalogFallback)
	}
}

func TestTryDeterministicJudge_misroutedOutOfScope(t *testing.T) {
	turn := AITriageTurn{
		Path:      PathOutOfScope,
		UserText:  "ditunggu ya",
		ReplyText: "Maaf kak, itu di luar topik bisnis kami ya.",
	}
	res := tryDeterministicJudge(turn, omahCatalog())
	if !res.Resolved || !res.Verdict.Flagged {
		t.Fatalf("expected deterministic flag, got %+v", res)
	}
}

func TestTryDeterministicJudge_genuineOutOfScopePass(t *testing.T) {
	turn := AITriageTurn{
		Path:      PathOutOfScope,
		UserText:  "berapa harga saham BBRI?",
		ReplyText: "Maaf kak, itu di luar topik bisnis kami ya.",
	}
	res := tryDeterministicJudge(turn, omahCatalog())
	if !res.Resolved || res.Verdict.Flagged {
		t.Fatalf("expected deterministic pass, got %+v", res)
	}
}

func TestTryDeterministicJudge_greetingPass(t *testing.T) {
	turn := AITriageTurn{
		Path:      PathGreeting,
		UserText:  "halo kak",
		ReplyText: "Halo! Ada yang bisa dibantu?",
	}
	res := tryDeterministicJudge(turn, omahCatalog())
	if !res.Resolved || res.Verdict.Flagged {
		t.Fatalf("expected greeting pass, got %+v", res)
	}
}

func TestTryDeterministicJudge_priceMismatch(t *testing.T) {
	catalog := []dbCatalogItem{{
		Name: "Abon Sapi 500G", SellPrice: 35000, SellUnit: "pcs",
	}}
	turn := AITriageTurn{
		Path:      PathOrderStatus,
		UserText:  "status pesanan",
		ReplyText: "Abon Sapi 500G total Rp999999",
	}
	res := tryDeterministicJudge(turn, catalog)
	if !res.Resolved || !res.Verdict.Flagged {
		t.Fatalf("expected price mismatch flag, got %+v", res)
	}
	if res.Verdict.Category != "hallucination" {
		t.Fatalf("category = %s want hallucination", res.Verdict.Category)
	}
}

func TestTryDeterministicJudge_unresolvedAmbiguous(t *testing.T) {
	turn := AITriageTurn{
		Path:      PathOrderStatus,
		UserText:  "bisa nambah orderan?",
		ReplyText: "Pesanan WB-123: Abon Sapi 500G x 1",
	}
	res := tryDeterministicJudge(turn, omahCatalog())
	if res.Resolved {
		t.Fatalf("expected unresolved ambiguous turn, got %+v", res)
	}
}

func TestParseJudgeBatchVerdict(t *testing.T) {
	raw := `[{"flagged":false,"severity":"low","category":"ok","reason":"ok"},{"flagged":true,"severity":"medium","category":"wrong_answer","reason":"tidak menjawab"}]`
	verdicts, err := parseJudgeBatchVerdict(raw, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !verdicts[1].Flagged || verdicts[1].Category != "wrong_answer" {
		t.Fatalf("unexpected second verdict: %+v", verdicts[1])
	}
}

func TestBuildJudgeBatchUserPrompt_includesTurns(t *testing.T) {
	turns := []AITriageTurn{
		{Path: PathGreeting, UserText: "halo", ReplyText: "Halo kak"},
		{Path: PathOrderStatus, UserText: "status WB-1", ReplyText: "Pesanan WB-1 diproses"},
	}
	prompt := buildJudgeBatchUserPrompt("Omah Apparel", omahCatalog(), turns)
	if !strings.Contains(prompt, "=== Turn 1 ===") || !strings.Contains(prompt, "=== Turn 2 ===") {
		t.Fatal("expected turn sections in batch prompt")
	}
	if !strings.Contains(prompt, "Katalog resmi") {
		t.Fatal("expected catalog section")
	}
}

func TestBuildJudgeUserPrompt_usesCompactCatalog(t *testing.T) {
	catalog := omahCatalog()
	turn := AITriageTurn{
		Path:      PathOrderStatus,
		UserText:  "WB-372AF9ED status pesanan e apa ?",
		ReplyText: "Pesanan WB-372AF9ED: Abon Sapi 500G x 3, total Rp105000",
	}
	compact := relevantCatalogForTurn(turn, catalog)
	prompt := buildJudgeUserPrompt("Omah Apparel", compact, turn)
	if !strings.Contains(prompt, "Abon Sapi 500G") {
		t.Fatal("expected catalog product in judge prompt")
	}
	if strings.Contains(prompt, "HELLO-KITTY") {
		t.Fatal("unexpected unrelated catalog item in compact prompt")
	}
}
