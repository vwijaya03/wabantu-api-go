package importcsv

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"encore.dev/beta/errs"

	"encore.app/wabantu/shared/types"
)

func staffUser() *types.AuthUser {
	return &types.AuthUser{AccountID: "acc_staff", TenantID: "ten_1", TenantSchema: "t_import_test", Role: "staff"}
}

type fakeStore struct {
	data      map[string][]byte
	published []ImportRequest
	deleted   []string
}

func newFakeStore(t *testing.T, jobID string, staged importStagingPayload) *fakeStore {
	t.Helper()
	raw, err := json.Marshal(staged)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeStore{data: map[string][]byte{importStagingKey(jobID): raw}}
}

func (s *fakeStore) get(_ context.Context, key string) ([]byte, error) {
	if b, ok := s.data[key]; ok {
		return b, nil
	}
	return nil, errors.New("redis: nil")
}

func (s *fakeStore) deps() executeDeps {
	return executeDeps{
		get: s.get,
		publish: func(_ context.Context, m ImportRequest) error {
			s.published = append(s.published, m)
			return nil
		},
		del: func(_ context.Context, key string) error {
			s.deleted = append(s.deleted, key)
			return nil
		},
	}
}

func catalogStaged() importStagingPayload {
	return importStagingPayload{
		TenantSchema: "t_import_test",
		TenantID:     "ten_1",
		TargetTable:  "business_catalog_item",
		Headers:      []string{"Nama", "Kode"},
		Rows:         [][]string{{"Kopi", "K1"}},
		UploadedBy:   "acc_owner",
	}
}

var catalogMapping = map[string]string{"Nama": "name", "Kode": "external_code"}

func errCode(err error) errs.ErrCode {
	var e *errs.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return errs.Unknown
}

func TestExecute_SameTarget_OK(t *testing.T) {
	for _, target := range []string{"business_catalog_item", ""} { // FE sends the preview target; empty = staged
		store := newFakeStore(t, "imp_a", catalogStaged())
		resp, err := executeImport(context.Background(), ownerUser(),
			&ExecuteRequest{JobID: "imp_a", TargetTable: target, ColumnMapping: catalogMapping}, store.deps())
		if err != nil {
			t.Fatalf("target %q: %v", target, err)
		}
		if resp.JobID != "imp_a" || len(store.published) != 1 || store.published[0].TargetTable != "business_catalog_item" {
			t.Fatalf("target %q: resp=%+v published=%+v", target, resp, store.published)
		}
		if store.published[0].TenantSchema != "t_import_test" || store.published[0].UploadedBy != "acc_owner" {
			t.Fatalf("published=%+v", store.published[0])
		}
		if len(store.deleted) != 1 || store.deleted[0] != importStagingKey("imp_a") {
			t.Fatalf("deleted=%v", store.deleted)
		}
	}
}

// QA finding 2: execute with knowledge_base_entry on a catalog preview returned 200.
func TestExecute_TargetMismatch_400(t *testing.T) {
	for _, target := range []string{"knowledge_base_entry", "contact", "BUSINESS_CATALOG_ITEM"} {
		store := newFakeStore(t, "imp_a", catalogStaged())
		_, err := executeImport(context.Background(), ownerUser(),
			&ExecuteRequest{JobID: "imp_a", TargetTable: target, ColumnMapping: map[string]string{"Nama": "question"}}, store.deps())
		if errCode(err) != errs.InvalidArgument {
			t.Fatalf("target %q: err=%v", target, err)
		}
		// Rejected by the staged-target check itself (not only the allowlist),
		// so it stays enforced if the allowlist ever grows.
		var e *errs.Error
		if !errors.As(err, &e) || e.Message != msgTargetMismatch {
			t.Fatalf("target %q: message=%v", target, err)
		}
		if len(store.published) != 0 || len(store.deleted) != 0 {
			t.Fatalf("target %q: must not publish/delete", target)
		}
	}
}

// A KB job staged before this change (or a forged staging entry) is no longer
// executable even when request and staged target agree.
func TestExecute_KBStaged_NotAllowlisted_400(t *testing.T) {
	staged := catalogStaged()
	staged.TargetTable = "knowledge_base_entry"
	for _, target := range []string{"knowledge_base_entry", ""} {
		store := newFakeStore(t, "imp_kb", staged)
		_, err := executeImport(context.Background(), ownerUser(),
			&ExecuteRequest{JobID: "imp_kb", TargetTable: target, ColumnMapping: map[string]string{"Nama": "question"}}, store.deps())
		if errCode(err) != errs.InvalidArgument || len(store.published) != 0 {
			t.Fatalf("target %q: err=%v published=%d", target, err, len(store.published))
		}
	}
}

func TestExecute_InvalidColumn_400(t *testing.T) {
	store := newFakeStore(t, "imp_a", catalogStaged())
	_, err := executeImport(context.Background(), ownerUser(),
		&ExecuteRequest{JobID: "imp_a", ColumnMapping: map[string]string{"Nama": "question"}}, store.deps())
	if errCode(err) != errs.InvalidArgument || len(store.published) != 0 {
		t.Fatalf("err=%v", err)
	}
}

func TestExecute_StaffForbidden_403(t *testing.T) {
	for _, u := range []*types.AuthUser{staffUser(), nil, {AccountID: "acc_sa", Role: "super_admin", TenantSchema: "t_import_test"}} {
		store := newFakeStore(t, "imp_a", catalogStaged())
		_, err := executeImport(context.Background(), u,
			&ExecuteRequest{JobID: "imp_a", ColumnMapping: catalogMapping}, store.deps())
		if errCode(err) != errs.PermissionDenied || len(store.published) != 0 {
			t.Fatalf("user %+v: err=%v published=%d", u, err, len(store.published))
		}
	}
}

func TestExecute_ImpersonatingSuperAdmin_OK(t *testing.T) {
	store := newFakeStore(t, "imp_a", catalogStaged())
	u := &types.AuthUser{AccountID: "acc_sa", Role: "super_admin", Impersonating: true, TenantID: "ten_1", TenantSchema: "t_import_test"}
	if _, err := executeImport(context.Background(), u,
		&ExecuteRequest{JobID: "imp_a", ColumnMapping: catalogMapping}, store.deps()); err != nil {
		t.Fatal(err)
	}
}

func resultStore(t *testing.T, jobID string) *fakeStore {
	t.Helper()
	raw, _ := encodeImportResult("ten_1", ImportResult{TotalRows: 2, SuccessCount: 2})
	return &fakeStore{data: map[string][]byte{importResultKey(jobID): raw}}
}

func TestImportJobStatus_Owner_OK(t *testing.T) {
	store := resultStore(t, "imp_a")
	res, err := importJobStatus(context.Background(), ownerUser(), "imp_a", store.get)
	if err != nil || res.SuccessCount != 2 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestImportJobStatus_StaffForbidden_403(t *testing.T) {
	store := resultStore(t, "imp_a")
	for _, u := range []*types.AuthUser{staffUser(), nil} {
		res, err := importJobStatus(context.Background(), u, "imp_a", store.get)
		if errCode(err) != errs.PermissionDenied || res != nil {
			t.Fatalf("user %+v: res=%+v err=%v", u, res, err)
		}
	}
}

func TestImportJobStatus_Missing_404(t *testing.T) {
	store := resultStore(t, "imp_a")
	if _, err := importJobStatus(context.Background(), ownerUser(), "imp_other", store.get); errCode(err) != errs.NotFound {
		t.Fatalf("err=%v", err)
	}
}
