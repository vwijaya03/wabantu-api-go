package ai

import (
	"strings"
	"testing"

	"encore.app/wabantu/aivision"
)

func TestShouldProcessImageContext(t *testing.T) {
	if !shouldProcessImageContext(&dbMessage{Type: "image", Body: ""}) {
		t.Fatal("uncaptioned image should process")
	}
	if shouldProcessImageContext(&dbMessage{Type: "image", Body: "   "}) {
		t.Fatal("whitespace caption should not process")
	}
	if shouldProcessImageContext(&dbMessage{Type: "image", Body: "punya barang ini?"}) {
		t.Fatal("captioned image should not process (3a handles)")
	}
	if shouldProcessImageContext(&dbMessage{Type: "text", Body: ""}) {
		t.Fatal("non-image should not process")
	}
	if shouldProcessImageContext(nil) {
		t.Fatal("nil message should not process")
	}
}

func TestMatchCatalogFromVision_confidenceThreshold(t *testing.T) {
	catalog := []dbCatalogItem{
		{ID: "abon", ExternalCode: "ABON-500", Name: "Abon Sapi 500G", SellPrice: 35000},
	}
	low := aivision.ProductImageMatchExtract{
		ProductName: "Abon Sapi",
		Confidence:  0.5,
	}
	if matchCatalogFromVision(low, catalog) != nil {
		t.Fatal("low confidence should not match")
	}
}

func TestMatchCatalogFromVision_skuHint(t *testing.T) {
	catalog := []dbCatalogItem{
		{ID: "abon", ExternalCode: "ABON-500", Name: "Abon Sapi 500G", SellPrice: 35000},
		{ID: "boxer", ExternalCode: "BOXER-3", Name: "[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT", SellPrice: 56900},
	}
	extract := aivision.ProductImageMatchExtract{
		SkuHint:    "ABON-500",
		Confidence: 0.9,
	}
	match := matchCatalogFromVision(extract, catalog)
	if match == nil || match.ID != "abon" {
		t.Fatalf("expected abon by SKU, got %+v", match)
	}
}

func TestMatchCatalogFromVision_productName(t *testing.T) {
	catalog := []dbCatalogItem{
		{ID: "abon", ExternalCode: "ABON-500", Name: "Abon Sapi 500G", SellPrice: 35000},
		{ID: "boxer", ExternalCode: "BOXER-3", Name: "[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT", SellPrice: 56900},
	}
	extract := aivision.ProductImageMatchExtract{
		ProductName: "abon sapi kemasan",
		Confidence:  0.92,
	}
	match := matchCatalogFromVision(extract, catalog)
	if match == nil || match.ID != "abon" {
		t.Fatalf("expected abon by name, got %+v", match)
	}
}

func TestMatchCatalogFromVision_noMatch(t *testing.T) {
	catalog := []dbCatalogItem{
		{ID: "abon", ExternalCode: "ABON-500", Name: "Abon Sapi 500G", SellPrice: 35000},
	}
	extract := aivision.ProductImageMatchExtract{
		ProductName: "sepatu lari nike",
		Confidence:  0.95,
	}
	if matchCatalogFromVision(extract, catalog) != nil {
		t.Fatal("unrelated product should not match")
	}
}

func TestImageContextFallbackMessage(t *testing.T) {
	if !strings.Contains(imageContextFallbackMsg, "gambar tanpa keterangan") {
		t.Fatalf("unexpected fallback copy: %q", imageContextFallbackMsg)
	}
	if !strings.Contains(imageContextFallbackMsg, "bantuan") {
		t.Fatalf("fallback should mention bantuan: %q", imageContextFallbackMsg)
	}
}

func TestImageContextJobFromPaymentProof(t *testing.T) {
	job := imageContextJobFromPaymentProof(&PaymentProofJob{
		TenantSchema:     "t_test",
		TenantID:         "tid",
		ConversationID:   "c1",
		ContactID:        "ct1",
		MessageID:        "m1",
		InboundMessageID: "m1",
	})
	if job == nil || job.TenantSchema != "t_test" || job.MessageID != "m1" {
		t.Fatalf("unexpected job: %+v", job)
	}
}
