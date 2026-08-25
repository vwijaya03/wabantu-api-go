package tenant

import (
	"context"
	"fmt"
	"strings"
	"time"

	"encore.app/wabantu/system"
)

// MigrateSchemasRequest selects tenants to patch.
// Mode "behind" enqueues tenants with schema_patch_version < current.
// Empty TenantIDs with no mode = all active tenants (async when >3).
type MigrateSchemasRequest struct {
	TenantIDs []string `json:"tenantIds,omitempty"`
	Mode      string   `json:"mode,omitempty"` // "selected" | "behind" | "" (all)
}

type SchemaMigrationTarget struct {
	TenantID   string
	SchemaName string
}

type SchemaMigrationItem struct {
	TenantID           string     `json:"tenantId"`
	SchemaName         string     `json:"schemaName"`
	OK                 bool       `json:"ok"`
	Error              string     `json:"error,omitempty"`
	SchemaMigratedAt   *time.Time `json:"schemaMigratedAt,omitempty"`
	SchemaPatchVersion int        `json:"schemaPatchVersion,omitempty"`
}

type MigrateSchemasResponse struct {
	Async     bool                      `json:"async,omitempty"`
	JobID     string                    `json:"jobId,omitempty"`
	Enqueued  int                       `json:"enqueued,omitempty"`
	Patched   int                       `json:"patched"`
	Failed    int                       `json:"failed"`
	Errors    []string                  `json:"errors,omitempty"`
	Results   []SchemaMigrationItem     `json:"results,omitempty"`
	CloudPrep *CloudMigrationPrepResult `json:"cloudPrep,omitempty"`
}

// RunMigrateAllTenantSchemas applies patches to every active tenant schema (async).
func RunMigrateAllTenantSchemas(ctx context.Context, migratedBy string) (*MigrateSchemasResponse, error) {
	prep, err := RunCloudMigrationPrep(ctx)
	if err != nil {
		return nil, err
	}
	enq, err := EnqueueSchemaMigration(ctx, nil, migratedBy)
	if err != nil {
		return nil, err
	}
	return &MigrateSchemasResponse{
		Async:     true,
		JobID:     enq.JobID,
		Enqueued:  enq.Enqueued,
		CloudPrep: prep,
	}, nil
}

// RunMigrateTenantSchemas applies patches synchronously (≤3 tenants) or enqueues a job.
func RunMigrateTenantSchemas(ctx context.Context, req *MigrateSchemasRequest, migratedBy string) (*MigrateSchemasResponse, error) {
	prep, err := RunCloudMigrationPrep(ctx)
	if err != nil {
		return nil, err
	}

	if req != nil && strings.TrimSpace(req.Mode) == "behind" {
		enq, err := EnqueueBehindSchemaMigration(ctx, migratedBy)
		if err != nil {
			return nil, err
		}
		return &MigrateSchemasResponse{
			Async:     true,
			JobID:     enq.JobID,
			Enqueued:  enq.Enqueued,
			CloudPrep: prep,
		}, nil
	}

	if !ShouldUseSyncMigration(req) {
		enq, err := EnqueueSchemaMigration(ctx, req, migratedBy)
		if err != nil {
			return nil, err
		}
		return &MigrateSchemasResponse{
			Async:     true,
			JobID:     enq.JobID,
			Enqueued:  enq.Enqueued,
			CloudPrep: prep,
		}, nil
	}

	targets, err := resolveSchemaMigrationTargets(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return &MigrateSchemasResponse{Results: []SchemaMigrationItem{}, CloudPrep: prep}, nil
	}

	resp := &MigrateSchemasResponse{
		Results:   make([]SchemaMigrationItem, 0, len(targets)),
		CloudPrep: prep,
	}
	for _, target := range targets {
		item := SchemaMigrationItem{
			TenantID:   target.TenantID,
			SchemaName: target.SchemaName,
		}
		if err := ProcessTenantSchemaMigration(ctx, target.TenantID, target.SchemaName, migratedBy); err != nil {
			item.OK = false
			item.Error = err.Error()
			resp.Failed++
			resp.Errors = append(resp.Errors, fmt.Sprintf("%s: %v", target.SchemaName, err))
			resp.Results = append(resp.Results, item)
			continue
		}
		ver, _ := getTenantSchemaPatchVersion(ctx, target.TenantID)
		item.OK = true
		item.SchemaPatchVersion = ver
		if ver >= CurrentSchemaPatchVersion {
			var migratedAt time.Time
			_ = system.DB.QueryRow(ctx, `
				SELECT schema_migrated_at FROM tenant_company WHERE tenant_id = $1::uuid`,
				target.TenantID,
			).Scan(&migratedAt)
			if !migratedAt.IsZero() {
				item.SchemaMigratedAt = &migratedAt
			}
		}
		resp.Patched++
		resp.Results = append(resp.Results, item)
	}
	return resp, nil
}

func resolveSchemaMigrationTargets(ctx context.Context, req *MigrateSchemasRequest) ([]SchemaMigrationTarget, error) {
	if req != nil && len(req.TenantIDs) > 0 {
		return listSchemaMigrationTargetsByTenantIDs(ctx, req.TenantIDs)
	}
	return listAllSchemaMigrationTargets(ctx)
}

func listAllSchemaMigrationTargets(ctx context.Context) ([]SchemaMigrationTarget, error) {
	rows, err := system.DB.Query(ctx, `
		SELECT tc.tenant_id::text, tc.schema_name
		FROM tenant_company tc
		JOIN tenant t ON t.id = tc.tenant_id
		WHERE tc.schema_name IS NOT NULL AND tc.schema_name <> ''
		  AND t.deleted_at IS NULL
		ORDER BY tc.schema_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSchemaMigrationTargets(rows)
}

func listSchemaMigrationTargetsByTenantIDs(ctx context.Context, tenantIDs []string) ([]SchemaMigrationTarget, error) {
	ids := make([]string, 0, len(tenantIDs))
	seen := map[string]bool{}
	for _, id := range tenantIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no valid tenant ids")
	}

	rows, err := system.DB.Query(ctx, `
		SELECT tc.tenant_id::text, tc.schema_name
		FROM tenant_company tc
		JOIN tenant t ON t.id = tc.tenant_id
		WHERE tc.tenant_id = ANY($1::uuid[])
		  AND tc.schema_name IS NOT NULL AND tc.schema_name <> ''
		  AND t.deleted_at IS NULL
		ORDER BY tc.schema_name`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	targets, err := scanSchemaMigrationTargets(rows)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no matching active tenants for requested ids")
	}
	return targets, nil
}

type migrationTargetScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanSchemaMigrationTargets(rows migrationTargetScanner) ([]SchemaMigrationTarget, error) {
	var out []SchemaMigrationTarget
	for rows.Next() {
		var t SchemaMigrationTarget
		if err := rows.Scan(&t.TenantID, &t.SchemaName); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func recordSchemaMigrationSuccess(ctx context.Context, tenantID, migratedBy string, patchVersion int) (*time.Time, int, error) {
	var migratedAt time.Time
	var version int
	migratedBy = strings.TrimSpace(migratedBy)
	if migratedBy == "" {
		err := system.DB.QueryRow(ctx, `
			UPDATE tenant_company
			SET schema_migrated_at = now(),
			    schema_migrated_by = NULL,
			    schema_patch_version = $2,
			    updated_at = now()
			WHERE tenant_id = $1::uuid
			RETURNING schema_migrated_at, schema_patch_version`, tenantID, patchVersion,
		).Scan(&migratedAt, &version)
		if err != nil {
			return nil, 0, err
		}
		return &migratedAt, version, nil
	}
	err := system.DB.QueryRow(ctx, `
		UPDATE tenant_company
		SET schema_migrated_at = now(),
		    schema_migrated_by = $2::uuid,
		    schema_patch_version = $3,
		    updated_at = now()
		WHERE tenant_id = $1::uuid
		RETURNING schema_migrated_at, schema_patch_version`,
		tenantID, migratedBy, patchVersion,
	).Scan(&migratedAt, &version)
	if err != nil {
		return nil, 0, err
	}
	return &migratedAt, version, nil
}

// LookupTenantIDBySchema returns tenant_id for a schema name (for lazy migrate).
func LookupTenantIDBySchema(ctx context.Context, schemaName string) (string, error) {
	var tenantID string
	err := system.DB.QueryRow(ctx, `
		SELECT tenant_id::text FROM tenant_company WHERE schema_name = $1`, schemaName,
	).Scan(&tenantID)
	return tenantID, err
}
