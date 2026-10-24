package importcsv

import (
	"encoding/json"
	"errors"
	"testing"

	"encore.dev/beta/errs"
)

func tenantErrCode(err error) errs.ErrCode {
	var e *errs.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return errs.Unknown
}

func storedResultFor(t *testing.T, tenantID string) []byte {
	t.Helper()
	raw, err := encodeImportResult(tenantID, ImportResult{TotalRows: 3, SuccessCount: 2, FailedCount: 1,
		Errors: []RowError{{Row: 4, Message: "external_code and name required"}}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestImportStatus_SameTenant_200(t *testing.T) {
	res, err := decodeImportResultForTenant(storedResultFor(t, "ten_A"), "ten_A")
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalRows != 3 || res.SuccessCount != 2 || res.FailedCount != 1 || len(res.Errors) != 1 {
		t.Fatalf("res=%+v", res)
	}
	// The API response must not grow a tenantId field.
	b, _ := json.Marshal(res)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if _, ok := m["tenantId"]; ok {
		t.Fatalf("tenantId leaked into response: %s", b)
	}
}

// QA finding 1: owner of tenant B read tenant A's import status (200).
func TestImportStatus_CrossTenant_404(t *testing.T) {
	res, err := decodeImportResultForTenant(storedResultFor(t, "ten_A"), "ten_B")
	if tenantErrCode(err) != errs.NotFound || res != nil {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	var e *errs.Error
	if errors.As(err, &e) && e.Message != msgImportJobNotFound {
		t.Fatalf("mismatch must look like not-found, got %q", e.Message)
	}
}

func TestImportStatus_MissingTenant_404(t *testing.T) {
	legacy, _ := json.Marshal(ImportResult{TotalRows: 1, SuccessCount: 1}) // written before this fix
	for name, tc := range map[string]struct {
		raw    []byte
		tenant string
	}{
		"legacy result without tenantId": {legacy, "ten_A"},
		"caller without tenant":          {storedResultFor(t, "ten_A"), ""},
		"both empty":                     {storedResultFor(t, ""), ""},
	} {
		if _, err := decodeImportResultForTenant(tc.raw, tc.tenant); tenantErrCode(err) != errs.NotFound {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
}

func TestImportStatus_CorruptResult_500(t *testing.T) {
	if _, err := decodeImportResultForTenant([]byte("{"), "ten_A"); tenantErrCode(err) != errs.Internal {
		t.Fatalf("err=%v", err)
	}
}

func TestStagingTenant(t *testing.T) {
	staged := &importStagingPayload{TenantSchema: "t_a", TenantID: "ten_A"}
	if err := checkStagingTenant(staged, "t_a", "ten_A"); err != nil {
		t.Fatalf("same tenant: %v", err)
	}
	for name, tc := range map[string]struct {
		p              *importStagingPayload
		schema, tenant string
	}{
		"other tenant id":       {staged, "t_a", "ten_B"},
		"other schema":          {staged, "t_b", "ten_A"},
		"other tenant":          {staged, "t_b", "ten_B"},
		"legacy no tenant id":   {&importStagingPayload{TenantSchema: "t_a"}, "t_a", "ten_A"},
		"caller no tenant id":   {staged, "t_a", ""},
		"both tenant ids empty": {&importStagingPayload{TenantSchema: "t_a"}, "t_a", ""},
	} {
		err := checkStagingTenant(tc.p, tc.schema, tc.tenant)
		var e *errs.Error
		if !errors.As(err, &e) || e.Code != errs.NotFound || e.Message != msgImportStagingNotFound {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
}

func TestStagingPayload_CarriesTenantID(t *testing.T) {
	b, _ := json.Marshal(importStagingPayload{TenantSchema: "t_a", TenantID: "ten_A"})
	var back importStagingPayload
	if err := json.Unmarshal(b, &back); err != nil || back.TenantID != "ten_A" {
		t.Fatalf("round trip: %+v err=%v", back, err)
	}
}
