package ai

import (
	"strings"
	"testing"
)

func TestValidateReplyAgainstCatalog_ok(t *testing.T) {
	catalog := []dbCatalogItem{{
		Name: "[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT - M", SellPrice: 56900, SellUnit: "pcs",
	}}
	reply := "Kak, [3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT harganya Rp56900/paket (isi 3 pcs)."
	v := validateReplyAgainstCatalog(reply, catalog)
	if !v.OK {
		t.Fatalf("expected ok, got %s", v.Reason)
	}
}

func TestValidateReplyAgainstCatalog_priceMismatch(t *testing.T) {
	catalog := []dbCatalogItem{{
		Name: "[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT - M", SellPrice: 56900, SellUnit: "pcs",
	}}
	reply := "Boxer mono spot Rp99999/pcs ya kak."
	v := validateReplyAgainstCatalog(reply, catalog)
	if v.OK {
		t.Fatal("expected price mismatch")
	}
}

func TestGroundLLMReply_fallback(t *testing.T) {
	profile := &dbBusinessProfile{BusinessName: "Omah", Tone: strPtr("casual")}
	catalog := []dbCatalogItem{{
		Name: "[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT - M", SellPrice: 56900, SellUnit: "pcs",
	}}
	history := []dbMessage{{Direction: "out", Body: "Harga boxer Rp56900/paket"}}
	bad := "Boxer mono spot cuma Rp99999/pcs, bahan katun premium."
	got, grounded, reason := groundLLMReply(bad, "itu harga per biji berapa", profile, catalog, history)
	if !grounded || reason == "" {
		t.Fatalf("expected grounded reply, reason=%s", reason)
	}
	if strings.Contains(got, "99999") {
		t.Fatalf("hallucinated price leaked: %s", got)
	}
}
