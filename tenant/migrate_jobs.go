package tenant

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"encore.app/wabantu/system"
	"encore.dev/pubsub"
	"encore.dev/rlog"
)

const (
	migrationJobStatusPending   = "pending"
	migrationJobStatusRunning   = "running"
	migrationJobStatusCompleted = "completed"
	migrationJobStatusCancelled = "cancelled"

	migrationItemStatusQueued    = "queued"
	migrationItemStatusRunning   = "running"
	migrationItemStatusSucceeded = "succeeded"
	migrationItemStatusFailed    = "failed"
	migrationItemStatusSkipped   = "skipped"

	migrationEnqueueBatchSize = 500
	syncMigrationMaxTenants   = 3
)

// TenantSchemaMigrateMessage is one tenant migration unit of work.
type TenantSchemaMigrateMessage struct {
	JobID        string `json:"jobId"`
	ItemID       string `json:"itemId"`
	TenantID     string `json:"tenantId"`
	SchemaName   string `json:"schemaName"`
	PatchVersion int    `json:"patchVersion"`
	MigratedBy   string `json:"migratedBy,omitempty"`
	Lazy         bool   `json:"lazy,omitempty"`
}

// SchemaMigrateTopic processes tenant schema patches asynchronously.
var SchemaMigrateTopic = pubsub.NewTopic[*TenantSchemaMigrateMessage]("tenant-schema-migrate", pubsub.TopicConfig{
	DeliveryGuarantee: pubsub.AtLeastOnce,
})

var _ = pubsub.NewSubscription(SchemaMigrateTopic, "tenant-schema-migrate-worker", pubsub.SubscriptionConfig[*TenantSchemaMigrateMessage]{
	Handler:     handleTenantSchemaMigrate,
	RetryPolicy: &pubsub.RetryPolicy{MaxRetries: 3},
})

// MigrateSchemasEnqueueResponse is returned when migration is enqueued (async).
type MigrateSchemasEnqueueResponse struct {
	Async    bool   `json:"async"`
	JobID    string `json:"jobId"`
	Enqueued int    `json:"enqueued"`
}

// SchemaMigrationJobSummary is job progress for admin polling.
type SchemaMigrationJobSummary struct {
	JobID        string     `json:"jobId"`
	PatchVersion int        `json:"patchVersion"`
	Status       string     `json:"status"`
	TotalCount   int        `json:"totalCount"`
	DoneCount    int        `json:"doneCount"`
	FailedCount  int        `json:"failedCount"`
	StartedBy    string     `json:"startedBy,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
	RecentErrors []string   `json:"recentErrors,omitempty"`
}

// EnqueueSchemaMigration creates a background job for tenant schema patches.
func EnqueueSchemaMigration(ctx context.Context, req *MigrateSchemasRequest, migratedBy string) (*MigrateSchemasEnqueueResponse, error) {
	targets, err := resolveSchemaMigrationTargets(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return &MigrateSchemasEnqueueResponse{Async: true, Enqueued: 0}, nil
	}

	filtered, err := filterTargetsNotInActiveJob(ctx, targets)
	if err != nil {
		return nil, err
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("semua tenant terpilih sudah dalam antrian migrasi aktif")
	}

	var startedBy any
	if strings.TrimSpace(migratedBy) != "" {
		startedBy = migratedBy
	}

	var jobID string
	err = system.DB.QueryRow(ctx, `
		INSERT INTO tenant_schema_migration_job (patch_version, status, total_count, started_by)
		VALUES ($1, $2, $3, $4)
		RETURNING id::text`,
		CurrentSchemaPatchVersion, migrationJobStatusPending, len(filtered), startedBy,
	).Scan(&jobID)
	if err != nil {
		return nil, err
	}

	enqueued := 0
	for _, target := range filtered {
		var itemID string
		err = system.DB.QueryRow(ctx, `
			INSERT INTO tenant_schema_migration_job_item (job_id, tenant_id, schema_name, status)
			VALUES ($1::uuid, $2::uuid, $3, $4)
			RETURNING id::text`,
			jobID, target.TenantID, target.SchemaName, migrationItemStatusQueued,
		).Scan(&itemID)
		if err != nil {
			return nil, err
		}
		msg := &TenantSchemaMigrateMessage{
			JobID:        jobID,
			ItemID:       itemID,
			TenantID:     target.TenantID,
			SchemaName:   target.SchemaName,
			PatchVersion: CurrentSchemaPatchVersion,
			MigratedBy:   migratedBy,
		}
		if _, err := SchemaMigrateTopic.Publish(ctx, msg); err != nil {
			return nil, fmt.Errorf("publish migration job: %w", err)
		}
		enqueued++
	}

	_, _ = system.DB.Exec(ctx, `
		UPDATE tenant_schema_migration_job SET status = $2 WHERE id = $1::uuid`,
		jobID, migrationJobStatusRunning,
	)

	return &MigrateSchemasEnqueueResponse{
		Async:    true,
		JobID:    jobID,
		Enqueued: enqueued,
	}, nil
}

// EnqueueBehindSchemaMigration enqueues all active tenants below CurrentSchemaPatchVersion.
func EnqueueBehindSchemaMigration(ctx context.Context, migratedBy string) (*MigrateSchemasEnqueueResponse, error) {
	var jobID string
	var startedBy any
	if strings.TrimSpace(migratedBy) != "" {
		startedBy = migratedBy
	}

	err := system.DB.QueryRow(ctx, `
		INSERT INTO tenant_schema_migration_job (patch_version, status, total_count, started_by)
		VALUES ($1, $2, 0, $3)
		RETURNING id::text`,
		CurrentSchemaPatchVersion, migrationJobStatusPending, startedBy,
	).Scan(&jobID)
	if err != nil {
		return nil, err
	}

	totalEnqueued := 0
	offset := 0
	for {
		targets, err := listBehindSchemaMigrationTargets(ctx, CurrentSchemaPatchVersion, migrationEnqueueBatchSize, offset)
		if err != nil {
			return nil, err
		}
		if len(targets) == 0 {
			break
		}

		filtered, err := filterTargetsNotInActiveJob(ctx, targets)
		if err != nil {
			return nil, err
		}

		for _, target := range filtered {
			var itemID string
			err = system.DB.QueryRow(ctx, `
				INSERT INTO tenant_schema_migration_job_item (job_id, tenant_id, schema_name, status)
				VALUES ($1::uuid, $2::uuid, $3, $4)
				RETURNING id::text`,
				jobID, target.TenantID, target.SchemaName, migrationItemStatusQueued,
			).Scan(&itemID)
			if err != nil {
				return nil, err
			}
			msg := &TenantSchemaMigrateMessage{
				JobID:        jobID,
				ItemID:       itemID,
				TenantID:     target.TenantID,
				SchemaName:   target.SchemaName,
				PatchVersion: CurrentSchemaPatchVersion,
				MigratedBy:   migratedBy,
			}
			if _, err := SchemaMigrateTopic.Publish(ctx, msg); err != nil {
				return nil, fmt.Errorf("publish migration job: %w", err)
			}
			totalEnqueued++
		}
		offset += len(targets)
		if len(targets) < migrationEnqueueBatchSize {
			break
		}
	}

	_, err = system.DB.Exec(ctx, `
		UPDATE tenant_schema_migration_job
		SET total_count = $2, status = $3
		WHERE id = $1::uuid`,
		jobID, totalEnqueued, migrationJobStatusRunning,
	)
	if err != nil {
		return nil, err
	}

	if totalEnqueued == 0 {
		_, _ = system.DB.Exec(ctx, `
			UPDATE tenant_schema_migration_job
			SET status = $2, completed_at = now()
			WHERE id = $1::uuid`,
			jobID, migrationJobStatusCompleted,
		)
	}

	return &MigrateSchemasEnqueueResponse{
		Async:    true,
		JobID:    jobID,
		Enqueued: totalEnqueued,
	}, nil
}

// GetSchemaMigrationJob returns job progress for admin UI polling.
func GetSchemaMigrationJob(ctx context.Context, jobID string) (*SchemaMigrationJobSummary, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil, fmt.Errorf("jobId required")
	}

	var summary SchemaMigrationJobSummary
	var startedBy sql.NullString
	var completedAt sql.NullTime
	err := system.DB.QueryRow(ctx, `
		SELECT id::text, patch_version, status, total_count, done_count, failed_count,
		       started_by::text, created_at, completed_at
		FROM tenant_schema_migration_job
		WHERE id = $1::uuid`, jobID,
	).Scan(
		&summary.JobID, &summary.PatchVersion, &summary.Status,
		&summary.TotalCount, &summary.DoneCount, &summary.FailedCount,
		&startedBy, &summary.CreatedAt, &completedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("migration job not found")
	}
	if err != nil {
		return nil, err
	}
	if startedBy.Valid {
		summary.StartedBy = startedBy.String
	}
	if completedAt.Valid {
		t := completedAt.Time
		summary.CompletedAt = &t
	}

	rows, err := system.DB.Query(ctx, `
		SELECT COALESCE(error_text, schema_name)
		FROM tenant_schema_migration_job_item
		WHERE job_id = $1::uuid AND status = $2
		ORDER BY updated_at DESC
		LIMIT 10`, jobID, migrationItemStatusFailed)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var line string
			if scanErr := rows.Scan(&line); scanErr == nil && line != "" {
				summary.RecentErrors = append(summary.RecentErrors, line)
			}
		}
	}

	return &summary, nil
}

// ShouldUseSyncMigration returns true when a small explicit tenant selection can run inline.
func ShouldUseSyncMigration(req *MigrateSchemasRequest) bool {
	if req == nil || len(req.TenantIDs) == 0 {
		return false
	}
	n := 0
	for _, id := range req.TenantIDs {
		if strings.TrimSpace(id) != "" {
			n++
		}
	}
	return n > 0 && n <= syncMigrationMaxTenants
}

// ProcessTenantSchemaMigration applies patches (or backfills version) for one tenant.
func ProcessTenantSchemaMigration(ctx context.Context, tenantID, schemaName, migratedBy string) error {
	provisioned, err := tenantSchemaBaseProvisioned(ctx, schemaName)
	if err != nil {
		return err
	}
	if !provisioned {
		rlog.Info("skip schema migration until tenant bootstrap completes",
			"tenantId", tenantID, "schema", schemaName)
		return nil
	}
	if err := RepairTenantSchemaDeployGrants(ctx, schemaName); err != nil {
		return fmt.Errorf("repair deploy grants: %w", err)
	}
	if err := applyCloudAdminTenantDDL(ctx, schemaName); err != nil {
		return fmt.Errorf("cloud admin DDL: %w", err)
	}

	// Module-specific evt_* DDL (new columns) is idempotent and must run even when
	// tenant schema_patch_version is already current.
	if err := RunEventsSchemaPatches(ctx, schemaName); err != nil {
		return err
	}

	ver, err := getTenantSchemaPatchVersion(ctx, tenantID)
	if err != nil {
		return err
	}
	if ver >= CurrentSchemaPatchVersion {
		return finalizeTenantSchemaMigration(ctx, schemaName)
	}

	if err := RunSchemaPatches(ctx, schemaName); err != nil {
		return err
	}
	if _, _, err = recordSchemaMigrationSuccess(ctx, tenantID, migratedBy, CurrentSchemaPatchVersion); err != nil {
		return err
	}
	return finalizeTenantSchemaMigration(ctx, schemaName)
}

func finalizeTenantSchemaMigration(ctx context.Context, schemaName string) error {
	if err := RepairTenantSchemaDeployGrants(ctx, schemaName); err != nil {
		return fmt.Errorf("finalize deploy grants: %w", err)
	}
	return nil
}

func handleTenantSchemaMigrate(ctx context.Context, msg *TenantSchemaMigrateMessage) error {
	if msg == nil || msg.TenantID == "" || msg.SchemaName == "" {
		return fmt.Errorf("invalid migration message")
	}

	// Lazy migrations without a job item run directly.
	if msg.Lazy || msg.ItemID == "" {
		return ProcessTenantSchemaMigration(ctx, msg.TenantID, msg.SchemaName, msg.MigratedBy)
	}

	_, err := system.DB.Exec(ctx, `
		UPDATE tenant_schema_migration_job_item
		SET status = $2, attempts = attempts + 1, updated_at = now()
		WHERE id = $1::uuid AND status = ANY($3::text[])`,
		msg.ItemID, migrationItemStatusRunning,
		[]string{migrationItemStatusQueued, migrationItemStatusRunning},
	)
	if err != nil {
		return err
	}

	procErr := ProcessTenantSchemaMigration(ctx, msg.TenantID, msg.SchemaName, msg.MigratedBy)
	if procErr != nil {
		_, _ = system.DB.Exec(ctx, `
			UPDATE tenant_schema_migration_job_item
			SET status = $2, error_text = $3, updated_at = now()
			WHERE id = $1::uuid`,
			msg.ItemID, migrationItemStatusFailed, procErr.Error(),
		)
		_ = incrementJobCounter(ctx, msg.JobID, false)
		rlog.Warn("tenant schema migration failed",
			"tenantId", msg.TenantID, "schema", msg.SchemaName, "err", procErr)
		return procErr
	}

	_, _ = system.DB.Exec(ctx, `
		UPDATE tenant_schema_migration_job_item
		SET status = $2, error_text = NULL, updated_at = now()
		WHERE id = $1::uuid`,
		msg.ItemID, migrationItemStatusSucceeded,
	)
	_ = incrementJobCounter(ctx, msg.JobID, true)
	return nil
}

func incrementJobCounter(ctx context.Context, jobID string, success bool) error {
	if jobID == "" {
		return nil
	}
	col := "failed_count"
	if success {
		col = "done_count"
	}
	_, err := system.DB.Exec(ctx, fmt.Sprintf(`
		UPDATE tenant_schema_migration_job
		SET %s = %s + 1
		WHERE id = $1::uuid`, col, col), jobID)
	if err != nil {
		return err
	}

	var total, done, failed int
	var status string
	err = system.DB.QueryRow(ctx, `
		SELECT total_count, done_count, failed_count, status
		FROM tenant_schema_migration_job WHERE id = $1::uuid`, jobID,
	).Scan(&total, &done, &failed, &status)
	if err != nil {
		return err
	}
	if status == migrationJobStatusCancelled {
		return nil
	}
	if total > 0 && done+failed >= total {
		_, err = system.DB.Exec(ctx, `
			UPDATE tenant_schema_migration_job
			SET status = $2, completed_at = now()
			WHERE id = $1::uuid AND status <> $2`,
			jobID, migrationJobStatusCompleted,
		)
	}
	return err
}

func listBehindSchemaMigrationTargets(ctx context.Context, maxVersion, limit, offset int) ([]SchemaMigrationTarget, error) {
	rows, err := system.DB.Query(ctx, `
		SELECT tc.tenant_id::text, tc.schema_name
		FROM tenant_company tc
		JOIN tenant t ON t.id = tc.tenant_id
		WHERE tc.schema_name IS NOT NULL AND tc.schema_name <> ''
		  AND t.deleted_at IS NULL
		  AND COALESCE(tc.schema_patch_version, 0) < $1
		ORDER BY tc.tenant_id
		LIMIT $2 OFFSET $3`, maxVersion, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSchemaMigrationTargets(rows)
}

func filterTargetsNotInActiveJob(ctx context.Context, targets []SchemaMigrationTarget) ([]SchemaMigrationTarget, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	ids := make([]string, len(targets))
	byID := make(map[string]SchemaMigrationTarget, len(targets))
	for i, t := range targets {
		ids[i] = t.TenantID
		byID[t.TenantID] = t
	}

	rows, err := system.DB.Query(ctx, `
		SELECT DISTINCT ji.tenant_id::text
		FROM tenant_schema_migration_job_item ji
		JOIN tenant_schema_migration_job j ON j.id = ji.job_id
		WHERE ji.tenant_id = ANY($1::uuid[])
		  AND ji.status = ANY($2::text[])
		  AND j.status = ANY($3::text[])`,
		ids,
		[]string{migrationItemStatusQueued, migrationItemStatusRunning},
		[]string{migrationJobStatusPending, migrationJobStatusRunning},
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	busy := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		busy[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]SchemaMigrationTarget, 0, len(targets))
	for _, t := range targets {
		if !busy[t.TenantID] {
			out = append(out, t)
		}
	}
	return out, nil
}

func getTenantSchemaPatchVersion(ctx context.Context, tenantID string) (int, error) {
	var ver int
	err := system.DB.QueryRow(ctx, `
		SELECT COALESCE(schema_patch_version, 0)
		FROM tenant_company WHERE tenant_id = $1::uuid`, tenantID,
	).Scan(&ver)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("tenant company not found")
	}
	return ver, err
}

// RecordNewTenantSchemaVersion marks a freshly provisioned tenant at the current patch level.
func RecordNewTenantSchemaVersion(ctx context.Context, tenantID string) error {
	_, _, err := recordSchemaMigrationSuccess(ctx, tenantID, "", CurrentSchemaPatchVersion)
	return err
}

var (
	lazyMigrateMu    sync.Mutex
	lazyMigrateUntil = map[string]time.Time{}
)

// PublishLazySchemaMigration enqueues a single-tenant migration if behind (non-blocking dedupe).
func PublishLazySchemaMigration(ctx context.Context, tenantID, schemaName string) {
	tenantID = strings.TrimSpace(tenantID)
	schemaName = strings.TrimSpace(schemaName)
	if tenantID == "" || schemaName == "" {
		return
	}

	lazyMigrateMu.Lock()
	if until, ok := lazyMigrateUntil[schemaName]; ok && time.Now().Before(until) {
		lazyMigrateMu.Unlock()
		return
	}
	lazyMigrateUntil[schemaName] = time.Now().Add(5 * time.Minute)
	lazyMigrateMu.Unlock()

	provisioned, err := tenantSchemaBaseProvisioned(ctx, schemaName)
	if err != nil || !provisioned {
		return
	}

	ver, err := getTenantSchemaPatchVersion(ctx, tenantID)
	if err != nil || ver >= CurrentSchemaPatchVersion {
		return
	}

	msg := &TenantSchemaMigrateMessage{
		TenantID:     tenantID,
		SchemaName:   schemaName,
		PatchVersion: CurrentSchemaPatchVersion,
		Lazy:         true,
	}
	if _, pubErr := SchemaMigrateTopic.Publish(ctx, msg); pubErr != nil {
		rlog.Warn("lazy schema migration publish failed", "tenantId", tenantID, "err", pubErr)
	}
}
