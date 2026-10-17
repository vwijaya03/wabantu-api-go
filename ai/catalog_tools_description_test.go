package ai

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// omahSteakToolCatalog mirrors internal/buyerflow/fixtures_omah_steak.go (same five products
// and prices) with product descriptions, as the incident tenant has them in business_catalog_item.
func omahSteakToolCatalog() []dbCatalogItem {
	return []dbCatalogItem{
		{ID: "beef-500", ExternalCode: "AUSTRALIAN_BEEF_500_GRAM", Name: "Australian Beef 500 Gram", SellPrice: 450000, SellUnit: "pcs",
			Description: "Australian beef cut, 500 grams per pack, vacuum-sealed and kept frozen."},
		{ID: "tbone-1500", ExternalCode: "AUSTRALIAN_BEEF_T_BONE_1_5_KG", Name: "Australian Beef T-Bone 1.5 Kg", SellPrice: 1750000, SellUnit: "pcs"},
		{ID: "salmon-100", ExternalCode: "SALMON_STEAK_100_GRAM", Name: "Salmon Steak 100 Gram", SellPrice: 75000},
		{ID: "salmon-250", ExternalCode: "SALMON_STEAK_250_GRAM_SALINAN_CZED", Name: "Salmon Steak 250 Gram", SellPrice: 105000},
		{ID: "tuna-100", ExternalCode: "YELLOW_TAIL_TUNA_PORTION_100_GRAM", Name: "Yellow Tail Tuna Portion 100 Gram", SellPrice: 350000, SellUnit: "pcs",
			Description: "Premium cut of frozen yellow tail tuna steak, 100 grams per portion. Sold frozen and vacuum-sealed to lock in freshness and premium quality."},
	}
}

// The buyer says "tuna steak" while the product name says "Tuna Portion"; only the
// description links the two, so it must lift the tuna item above "Salmon Steak" rows.
func TestSearchCatalogItemsMatchesDescription(t *testing.T) {
	hits := searchCatalogItems("tuna steak", omahSteakToolCatalog(), 3)
	if len(hits) == 0 {
		t.Fatal("expected hits for 'tuna steak'")
	}
	if hits[0].Name != "Yellow Tail Tuna Portion 100 Gram" {
		names := make([]string, len(hits))
		for i, h := range hits {
			names[i] = h.Name
		}
		t.Fatalf("top hit = %q, want the Yellow Tail Tuna item; ranking: %v", hits[0].Name, names)
	}
}

// A description-only match must never outrank an item whose name matches the whole query.
func TestSearchCatalogItemsNameMatchBeatsDescriptionOnly(t *testing.T) {
	hits := searchCatalogItems("salmon steak", omahSteakToolCatalog(), 3)
	if len(hits) == 0 || !strings.HasPrefix(hits[0].Name, "Salmon Steak") {
		t.Fatalf("name match must stay on top, got %+v", hits)
	}
}

func TestCatalogToolResultsIncludeDescription(t *testing.T) {
	exec := NewCatalogToolExecutor(omahSteakToolCatalog())

	out, err := exec.Run(catalogToolSearch, json.RawMessage(`{"query":"tuna steak","limit":3}`))
	if err != nil {
		t.Fatal(err)
	}
	var search struct {
		Found bool `json:"found"`
		Items []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &search); err != nil {
		t.Fatalf("search output is not JSON: %v\n%s", err, out)
	}
	if !search.Found || len(search.Items) == 0 {
		t.Fatalf("search should find the tuna item: %s", out)
	}
	if !strings.Contains(search.Items[0].Description, "frozen yellow tail tuna steak") {
		t.Fatalf(`search_catalog result lacks "description": %s`, out)
	}

	out, err = exec.Run(catalogToolGetProduct, json.RawMessage(`{"ref":"Yellow Tail Tuna Portion 100 Gram"}`))
	if err != nil {
		t.Fatal(err)
	}
	var get struct {
		Found   bool `json:"found"`
		Product struct {
			Description string `json:"description"`
		} `json:"product"`
	}
	if err := json.Unmarshal([]byte(out), &get); err != nil {
		t.Fatalf("get_product output is not JSON: %v\n%s", err, out)
	}
	if !get.Found || !strings.Contains(get.Product.Description, "vacuum-sealed") {
		t.Fatalf(`get_product result lacks "description": %s`, out)
	}
}

func TestFormatCatalogToolItemTruncatesDescription(t *testing.T) {
	long := strings.Repeat("é", 400) // multi-byte runes: a byte cut would split one
	it := dbCatalogItem{Name: "Tuna", SellPrice: 350000, Description: long}
	got := formatCatalogToolItem(&it)
	if n := utf8.RuneCountInString(got.Description); n > 301 { // 300 runes + the ellipsis
		t.Fatalf("description not truncated to 300 runes: %d runes", n)
	}
	if !utf8.ValidString(got.Description) {
		t.Fatal("truncation split a UTF-8 rune")
	}
	if short := formatCatalogToolItem(&dbCatalogItem{Name: "Tuna", SellPrice: 1, Description: "frozen"}); short.Description != "frozen" {
		t.Fatalf("short description must be copied as is, got %q", short.Description)
	}
}

// A miss is neutral JSON so the model answers in the buyer's language; the tool must not
// hand it an Indonesian sentence to echo (it used to, and groundLLMReply then forced it out).
func TestSearchCatalogMissIsNeutralJSON(t *testing.T) {
	exec := NewCatalogToolExecutor(omahSteakToolCatalog())

	searchMiss, err := exec.Run(catalogToolSearch, json.RawMessage(`{"query":"kasur busa","limit":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if searchMiss != `{"found":false,"items":[]}` {
		t.Fatalf("search miss = %s, want exactly {\"found\":false,\"items\":[]}", searchMiss)
	}

	getMiss, err := exec.Run(catalogToolGetProduct, json.RawMessage(`{"ref":"kasur busa"}`))
	if err != nil {
		t.Fatal(err)
	}
	if getMiss != `{"found":false}` {
		t.Fatalf("get_product miss = %s, want exactly {\"found\":false}", getMiss)
	}

	for _, o := range []string{searchMiss, getMiss} {
		for _, word := range []string{"message", "belum", "katalog", "saya"} {
			if strings.Contains(strings.ToLower(o), word) {
				t.Fatalf("miss output must carry no prose (%q found): %s", word, o)
			}
		}
	}
}

// Old tool output (and older LLM replies) carried the Indonesian sentence; keep recognising it.
func TestIsGenericCatalogNotFoundStillRecognisesIndonesianSentence(t *testing.T) {
	if !isGenericCatalogNotFound("Saya belum menemukan data tersebut di katalog saat ini.") {
		t.Fatal("legacy Indonesian not-found sentence must still be recognised")
	}
}
