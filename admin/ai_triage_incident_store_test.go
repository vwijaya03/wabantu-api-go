package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"encore.app/wabantu/shared/triageincident"
)

func TestIsNoRows_wrappedEncore(t *testing.T) {
	wrapped := fmt.Errorf("encore db: %w", sql.ErrNoRows)
	if wrapped == sql.ErrNoRows {
		t.Fatal("direct == must not match wrapped ErrNoRows")
	}
	if !isNoRows(wrapped) {
		t.Fatal("isNoRows must treat Encore-wrapped ErrNoRows as missing row")
	}
	if isNoRows(errors.New("other")) {
		t.Fatal("isNoRows must not match unrelated errors")
	}
}

func TestInsertOrLinkIncident_emptyTableCreates(t *testing.T) {
	ctx := context.Background()
	src := fmt.Sprintf("test-abon-%d", time.Now().UnixNano())
	inc, err := insertOrLinkIncident(ctx, triageincident.Incident{
		TenantID:        "11111111-1111-1111-1111-111111111111",
		TenantSchema:    "t_omah_apparel",
		Channel:         "whatsapp",
		Fingerprint:     fmt.Sprintf("%064d", time.Now().UnixNano()%1_000_000_000),
		Lane:            triageincident.LaneGroundedContent,
		EvidenceVersion: 1,
		Evidence:        []byte(`{"userText":"hah ? nambah abon sapi 125 gram 1","finalText":"Saya belum menemukan data tersebut di katalog saat ini.","path":"llm_grounded"}`),
	}, triageincident.SourceHumanReport, src, "whatsapp")
	if err != nil {
		t.Fatalf("insertOrLinkIncident: %v", err)
	}
	if strings.TrimSpace(inc.ID) == "" {
		t.Fatal("expected incident id")
	}
	again, err := insertOrLinkIncident(ctx, triageincident.Incident{
		TenantID:        inc.TenantID,
		TenantSchema:    inc.TenantSchema,
		Channel:         "whatsapp",
		Fingerprint:     inc.Fingerprint,
		Lane:            inc.Lane,
		EvidenceVersion: 1,
		Evidence:        []byte(`{"userText":"second"}`),
	}, triageincident.SourceHumanReport, src, "whatsapp")
	if err != nil {
		t.Fatalf("second insertOrLinkIncident: %v", err)
	}
	if again.ID != inc.ID {
		t.Fatalf("same source must link existing incident, got %q want %q", again.ID, inc.ID)
	}
}
