package ai

import (
	"strings"
	"testing"
)

func TestBuildJudgeUserPrompt_includesCatalog(t *testing.T) {
	catalog := omahCatalog()
	turn := AITriageTurn{
		Path:      PathOrderStatus,
		UserText:  "WB-372AF9ED status pesanan e apa ?",
		ReplyText: "Pesanan WB-372AF9ED: Abon Sapi 500G x 3, total Rp105000",
	}
	prompt := buildJudgeUserPrompt("Omah Apparel", catalog, turn)
	if !strings.Contains(prompt, "Abon Sapi 500G") {
		t.Fatal("expected catalog product in judge prompt")
	}
	if !strings.Contains(prompt, "Katalog resmi") {
		t.Fatal("expected catalog section header in judge prompt")
	}
	if !strings.Contains(prompt, "WB-372AF9ED status pesanan") {
		t.Fatal("expected customer message in judge prompt")
	}
}

func TestSoftenCatalogHallucinationVerdict_clearsFalsePositive(t *testing.T) {
	catalog := omahCatalog()
	v := llmJudgeVerdict{
		Flagged:  true,
		Severity: "high",
		Category: "hallucination",
		Reason:   "Produk Abon Sapi tidak sesuai bisnis apparel",
	}
	reply := "Pesanan WB-372AF9ED: Abon Sapi 500G x 3, total Rp105000"
	softenCatalogHallucinationVerdict(&v, reply, catalog)
	if v.Flagged {
		t.Fatalf("expected flag cleared, got %+v", v)
	}
	if v.Category != "ok" {
		t.Fatalf("category = %s want ok", v.Category)
	}
}

func TestSoftenCatalogHallucinationVerdict_keepsUnknownProduct(t *testing.T) {
	catalog := omahCatalog()
	v := llmJudgeVerdict{
		Flagged:  true,
		Severity: "high",
		Category: "hallucination",
		Reason:   "Produk tidak ada di katalog",
	}
	reply := "Stok iPhone 15 Pro Max ready ya kak"
	softenCatalogHallucinationVerdict(&v, reply, catalog)
	if !v.Flagged {
		t.Fatal("expected hallucination flag kept for unknown product")
	}
}

func TestSoftenCatalogHallucinationVerdict_keepsWrongAnswer(t *testing.T) {
	catalog := omahCatalog()
	v := llmJudgeVerdict{
		Flagged:  true,
		Severity: "high",
		Category: "wrong_answer",
		Reason:   "Status pengiriman salah",
	}
	reply := "Pesanan Abon Sapi 500G sudah sampai di alamat"
	softenCatalogHallucinationVerdict(&v, reply, catalog)
	if !v.Flagged {
		t.Fatal("wrong_answer should not be auto-cleared by catalog guard")
	}
}

func TestReconcileJudgeVerdict_unansweredQuestion(t *testing.T) {
	v := llmJudgeVerdict{
		Flagged:  false,
		Category: "ok",
		Severity: "low",
		Reason:   "Bot tidak menjawab pertanyaan 'bisa nambah orderan?' tetapi menampilkan status pesanan",
	}
	reconcileJudgeVerdict(&v)
	if !v.Flagged {
		t.Fatal("expected flag when reason mentions unanswered question")
	}
	if v.Category != "wrong_answer" {
		t.Fatalf("category = %s want wrong_answer", v.Category)
	}
}

func TestReconcileJudgeVerdict_keepsConsistentOk(t *testing.T) {
	v := llmJudgeVerdict{
		Flagged:  false,
		Category: "ok",
		Reason:   "Balasan sesuai dan menjawab pertanyaan status pesanan",
	}
	reconcileJudgeVerdict(&v)
	if v.Flagged {
		t.Fatal("expected ok verdict unchanged")
	}
}

func TestEnforceMisroutedOutOfScope_ditunggu(t *testing.T) {
	v := llmJudgeVerdict{Flagged: false, Category: "ok"}
	turn := AITriageTurn{
		Path:     PathOutOfScope,
		UserText: "ditunggu ya",
		ReplyText: "Maaf kak, itu di luar topik bisnis kami ya.",
	}
	enforceMisroutedOutOfScope(&v, turn)
	if !v.Flagged {
		t.Fatal("expected flag for misrouted out_of_scope")
	}
	if v.Category != "wrong_answer" {
		t.Fatalf("category = %s want wrong_answer", v.Category)
	}
}

func TestEnforceMisroutedOutOfScope_keepsRealOutOfScope(t *testing.T) {
	v := llmJudgeVerdict{Flagged: false, Category: "ok"}
	turn := AITriageTurn{
		Path:     PathOutOfScope,
		UserText: "berapa harga saham BBRI hari ini?",
		ReplyText: "Maaf kak, itu di luar topik bisnis kami ya.",
	}
	enforceMisroutedOutOfScope(&v, turn)
	if v.Flagged {
		t.Fatal("expected no flag for genuine out_of_scope question")
	}
}
