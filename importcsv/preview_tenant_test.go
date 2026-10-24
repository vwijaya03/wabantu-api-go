package importcsv

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"encore.dev/beta/errs"

	"encore.app/wabantu/shared/types"
)

// Preview (raw HTTP handler) -> staging -> execute, sharing one fake Redis.
// Fails if servePreview stops filling TenantID: checkStagingTenant would then
// turn every execute into 404 even for the tenant that uploaded the file.
func TestPreviewThenExecute_StagingCarriesTenantID(t *testing.T) {
	owner := ownerUser()
	store := &fakeStore{data: map[string][]byte{}}
	deps := testDeps(owner, &stageRecorder{})
	deps.stage = func(_ context.Context, jobID string, raw []byte) error {
		store.data[importStagingKey(jobID)] = raw
		return nil
	}

	rr := do(t, uploadReq(t, "katalog.csv", []byte("Nama,Kode\nKopi,K1\nTeh,T1\n")), deps)
	if rr.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", rr.Code, rr.Body.String())
	}
	var prev PreviewResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &prev); err != nil {
		t.Fatal(err)
	}

	raw, ok := store.data[importStagingKey(prev.JobID)]
	if !ok {
		t.Fatalf("nothing staged for %s", prev.JobID)
	}
	var staged importStagingPayload
	if err := json.Unmarshal(raw, &staged); err != nil {
		t.Fatal(err)
	}
	if staged.TenantID != owner.TenantID || staged.TenantSchema != owner.TenantSchema {
		t.Fatalf("staged tenant = (%q,%q), want (%q,%q)", staged.TenantID, staged.TenantSchema, owner.TenantID, owner.TenantSchema)
	}

	// Another tenant cannot execute it (404, indistinguishable from unknown).
	other := &types.AuthUser{AccountID: "acc_b", TenantID: "ten_2", TenantSchema: "t_other", Role: "owner"}
	if _, err := executeImport(context.Background(), other,
		&ExecuteRequest{JobID: prev.JobID, TargetTable: prev.TargetTable, ColumnMapping: catalogMapping}, store.deps()); errCode(err) != errs.NotFound {
		t.Fatalf("cross-tenant execute err=%v, want NotFound", err)
	}

	// The uploading owner can (FE sends the same targetTable to preview and execute).
	resp, err := executeImport(context.Background(), owner,
		&ExecuteRequest{JobID: prev.JobID, TargetTable: prev.TargetTable, ColumnMapping: catalogMapping}, store.deps())
	if err != nil {
		t.Fatalf("same-tenant execute: %v (code %v)", err, errCode(err))
	}
	if resp.JobID != prev.JobID || len(store.published) != 1 || store.published[0].TenantID != owner.TenantID {
		t.Fatalf("resp=%+v published=%+v", resp, store.published)
	}
}
