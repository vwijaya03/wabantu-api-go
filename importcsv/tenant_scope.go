package importcsv

import (
	"encoding/json"

	"encore.dev/beta/errs"
)

// Tenant isolation for import jobs. Every staged preview and every stored
// worker result carries the tenant that created it; status and execute only
// return it to the same tenant. A mismatch or a missing tenant answers 404
// with the same message as "not found", so a jobId from another tenant is
// indistinguishable from an unknown one.

const (
	msgImportJobNotFound     = "import job not found or still processing"
	msgImportStagingNotFound = "import preview expired or not found — upload again"
)

// storedImportResult is what handleImport writes to import:result:<jobId>.
// ImportResult is embedded so the JSON stays flat; only ImportResult is
// returned to clients.
type storedImportResult struct {
	TenantID string `json:"tenantId"`
	ImportResult
}

func encodeImportResult(tenantID string, result ImportResult) ([]byte, error) {
	return json.Marshal(storedImportResult{TenantID: tenantID, ImportResult: result})
}

// decodeImportResultForTenant returns the result only if it belongs to
// tenantID. Results written before this change have no tenantId and are
// treated as not found (they expire after 24h anyway).
func decodeImportResultForTenant(raw []byte, tenantID string) (*ImportResult, error) {
	var stored storedImportResult
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "invalid import result"}
	}
	if tenantID == "" || stored.TenantID == "" || stored.TenantID != tenantID {
		return nil, &errs.Error{Code: errs.NotFound, Message: msgImportJobNotFound}
	}
	result := stored.ImportResult
	return &result, nil
}

// checkStagingTenant rejects a staged preview that is not owned by the
// caller's tenant (ID and schema), or that predates tenant tagging.
func checkStagingTenant(p *importStagingPayload, tenantSchema, tenantID string) error {
	if tenantID == "" || p.TenantID == "" || p.TenantID != tenantID || p.TenantSchema != tenantSchema {
		return &errs.Error{Code: errs.NotFound, Message: msgImportStagingNotFound}
	}
	return nil
}
