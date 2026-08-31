package flag

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"encore.dev/pubsub"
	"encore.dev/rlog"

	"encore.app/wabantu/shared/retrieval"
	"encore.app/wabantu/tenant"
)

const (
	ragRolloutStatusPending   = "pending"
	ragRolloutStatusRunning   = "running"
	ragRolloutStatusCompleted = "completed"
	ragRolloutStatusCancelled = "cancelled"

	ragRolloutItemQueued    = "queued"
	ragRolloutItemRunning   = "running"
	ragRolloutItemSucceeded = "succeeded"
	ragRolloutItemFailed    = "failed"
	ragRolloutItemSkipped   = "skipped"

	ragRolloutScopeSelected    = "selected"
	ragRolloutScopeAllActive   = "all_active"
	ragRolloutScopeLexicalOnly = "lexical_only"

	defaultRolloutTenantDelayMs = 2000
	maxRolloutTenantDelayMs     = 30000
)

type ragRolloutTarget struct {
	TenantID   string
	SchemaName string
}

// RAGRolloutMessage processes one tenant in a bulk rollout job.
type RAGRolloutMessage struct {
	JobID         string    `json:"jobId"`
	ItemID        string    `json:"itemId"`
	TenantID      string    `json:"tenantId"`
	SchemaName    string    `json:"schemaName"`
	Mode          string    `json:"mode"`
	TenantDelayMs int       `json:"tenantDelayMs"`
	NotBefore     time.Time `json:"notBefore,omitempty"`
}

var RAGRolloutTopic = pubsub.NewTopic[*RAGRolloutMessage]("rag-retrieval-rollout", pubsub.TopicConfig{
	DeliveryGuarantee: pubsub.AtLeastOnce,
})

var _ = pubsub.NewSubscription(RAGRolloutTopic, "rag-retrieval-rollout-worker", pubsub.SubscriptionConfig[*RAGRolloutMessage]{
	Handler:     handleRAGRolloutMessage,
	RetryPolicy: &pubsub.RetryPolicy{MaxRetries: 3},
})

type StartRAGRolloutRequest struct {
	Mode          string   `json:"mode"` // shadow | vector
	Scope         string   `json:"scope"` // selected | all_active | lexical_only
	TenantIDs     []string `json:"tenantIds,omitempty"`
	TenantDelayMs *int     `json:"tenantDelayMs,omitempty"`
}

type StartRAGRolloutResponse struct {
	JobID    string `json:"jobId"`
	Enqueued int    `json:"enqueued"`
}

type RAGRolloutJobSummary struct {
	JobID               string     `json:"jobId"`
	Mode                string     `json:"mode"`
	Scope               string     `json:"scope"`
	Status              string     `json:"status"`
	TotalCount          int        `json:"totalCount"`
	DoneCount           int        `json:"doneCount"`
	FailedCount         int        `json:"failedCount"`
	KBEnqueuedTotal     int64      `json:"kbEnqueuedTotal"`
	CatalogEnqueuedTotal int64     `json:"catalogEnqueuedTotal"`
	TenantDelayMs       int        `json:"tenantDelayMs"`
	StartedBy           string     `json:"startedBy,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
	CompletedAt         *time.Time `json:"completedAt,omitempty"`
	RecentErrors        []string   `json:"recentErrors,omitempty"`
}

type ListRAGRolloutJobsResponse struct {
	Jobs []RAGRolloutJobSummary `json:"jobs"`
}

func enqueueRAGRollout(ctx context.Context, req *StartRAGRolloutRequest, startedBy string) (*StartRAGRolloutResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request required")
	}
	mode := strings.TrimSpace(req.Mode)
	if mode != string(retrieval.ModeShadow) && mode != string(retrieval.ModeVector) {
		return nil, fmt.Errorf("mode harus shadow atau vector")
	}
	scope := strings.TrimSpace(req.Scope)
	if scope == "" {
		scope = ragRolloutScopeSelected
	}
	if scope != ragRolloutScopeSelected && scope != ragRolloutScopeAllActive && scope != ragRolloutScopeLexicalOnly {
		return nil, fmt.Errorf("scope tidak valid")
	}
	if scope == ragRolloutScopeSelected && len(req.TenantIDs) == 0 {
		return nil, fmt.Errorf("tenantIds wajib untuk scope selected")
	}

	delay := defaultRolloutTenantDelayMs
	if req.TenantDelayMs != nil {
		delay = *req.TenantDelayMs
	}
	if delay < 500 {
		delay = 500
	}
	if delay > maxRolloutTenantDelayMs {
		delay = maxRolloutTenantDelayMs
	}

	if err := ensureNoActiveRAGRolloutJob(ctx); err != nil {
		return nil, err
	}

	targets, err := resolveRAGRolloutTargets(ctx, scope, req.TenantIDs)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return &StartRAGRolloutResponse{JobID: "", Enqueued: 0}, nil
	}

	filtered, err := filterRAGRolloutTargetsNotBusy(ctx, targets)
	if err != nil {
		return nil, err
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("semua tenant terpilih sudah dalam antrian rollout aktif")
	}

	var startedByArg any
	if strings.TrimSpace(startedBy) != "" {
		startedByArg = startedBy
	}

	var jobID string
	err = db.QueryRow(ctx, `
		INSERT INTO rag_rollout_job (mode, scope, status, total_count, tenant_delay_ms, started_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id::text`,
		mode, scope, ragRolloutStatusPending, len(filtered), delay, startedByArg,
	).Scan(&jobID)
	if err != nil {
		return nil, err
	}

	enqueued := 0
	for i, t := range filtered {
		n, err := enqueueRAGRolloutTarget(ctx, jobID, t, mode, delay, i)
		if err != nil {
			return nil, err
		}
		enqueued += n
	}
	if err := syncRAGRolloutJobAfterEnqueue(ctx, jobID); err != nil {
		return nil, err
	}

	return &StartRAGRolloutResponse{JobID: jobID, Enqueued: enqueued}, nil
}

func ensureNoActiveRAGRolloutJob(ctx context.Context) error {
	var n int
	err := db.QueryRow(ctx, `
		SELECT COUNT(*) FROM rag_rollout_job
		WHERE status = ANY($1::text[])`,
		[]string{ragRolloutStatusPending, ragRolloutStatusRunning},
	).Scan(&n)
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("masih ada job rollout RAG aktif — tunggu selesai atau batalkan dulu")
	}
	return nil
}

func resolveRAGRolloutTargets(ctx context.Context, scope string, tenantIDs []string) ([]ragRolloutTarget, error) {
	switch scope {
	case ragRolloutScopeSelected:
		return loadRAGRolloutTargetsByIDs(ctx, tenantIDs)
	case ragRolloutScopeAllActive, ragRolloutScopeLexicalOnly:
		all, err := listActiveRAGRolloutTargets(ctx)
		if err != nil {
			return nil, err
		}
		if scope == ragRolloutScopeAllActive {
			return all, nil
		}
		out := make([]ragRolloutTarget, 0, len(all))
		for _, t := range all {
			if RetrievalMode(ctx, t.TenantID) == retrieval.ModeDisabled {
				out = append(out, t)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("scope tidak dikenal")
	}
}

func loadRAGRolloutTargetsByIDs(ctx context.Context, tenantIDs []string) ([]ragRolloutTarget, error) {
	ids := make([]string, 0, len(tenantIDs))
	for _, id := range tenantIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := db.Query(ctx, `
		SELECT tc.tenant_id::text, tc.schema_name
		FROM tenant_company tc
		JOIN tenant t ON t.id = tc.tenant_id
		WHERE tc.tenant_id = ANY($1::uuid[])
		  AND t.deleted_at IS NULL
		  AND t.status = 'active'
		  AND tc.schema_name IS NOT NULL AND tc.schema_name <> ''`,
		ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRAGRolloutTargets(rows)
}

func listActiveRAGRolloutTargets(ctx context.Context) ([]ragRolloutTarget, error) {
	rows, err := db.Query(ctx, `
		SELECT tc.tenant_id::text, tc.schema_name
		FROM tenant_company tc
		JOIN tenant t ON t.id = tc.tenant_id
		WHERE t.deleted_at IS NULL
		  AND t.status = 'active'
		  AND tc.schema_name IS NOT NULL AND tc.schema_name <> ''
		ORDER BY tc.tenant_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRAGRolloutTargets(rows)
}

func scanRAGRolloutTargets(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]ragRolloutTarget, error) {
	var out []ragRolloutTarget
	for rows.Next() {
		var t ragRolloutTarget
		if err := rows.Scan(&t.TenantID, &t.SchemaName); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func filterRAGRolloutTargetsNotBusy(ctx context.Context, targets []ragRolloutTarget) ([]ragRolloutTarget, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	ids := make([]string, len(targets))
	byID := make(map[string]ragRolloutTarget, len(targets))
	for i, t := range targets {
		ids[i] = t.TenantID
		byID[t.TenantID] = t
	}
	rows, err := db.Query(ctx, `
		SELECT DISTINCT ji.tenant_id::text
		FROM rag_rollout_job_item ji
		JOIN rag_rollout_job j ON j.id = ji.job_id
		WHERE ji.tenant_id = ANY($1::uuid[])
		  AND ji.status = ANY($2::text[])
		  AND j.status = ANY($3::text[])`,
		ids,
		[]string{ragRolloutItemQueued, ragRolloutItemRunning},
		[]string{ragRolloutStatusPending, ragRolloutStatusRunning},
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
	out := make([]ragRolloutTarget, 0, len(targets))
	for _, t := range targets {
		if !busy[t.TenantID] {
			out = append(out, t)
		}
	}
	return out, nil
}

func enqueueRAGRolloutTarget(ctx context.Context, jobID string, target ragRolloutTarget, mode string, delayMs, sequenceIndex int) (int, error) {
	var itemID string
	err := db.QueryRow(ctx, `
		INSERT INTO rag_rollout_job_item (job_id, tenant_id, schema_name, status)
		VALUES ($1::uuid, $2::uuid, $3, $4)
		RETURNING id::text`,
		jobID, target.TenantID, target.SchemaName, ragRolloutItemQueued,
	).Scan(&itemID)
	if err != nil {
		return 0, err
	}
	notBefore := time.Now().Add(time.Duration(sequenceIndex*delayMs) * time.Millisecond)
	msg := &RAGRolloutMessage{
		JobID:         jobID,
		ItemID:        itemID,
		TenantID:      target.TenantID,
		SchemaName:    target.SchemaName,
		Mode:          mode,
		TenantDelayMs: delayMs,
		NotBefore:     notBefore,
	}
	if _, err := RAGRolloutTopic.Publish(ctx, msg); err != nil {
		return 0, fmt.Errorf("publish rollout: %w", err)
	}
	return 1, nil
}

func syncRAGRolloutJobAfterEnqueue(ctx context.Context, jobID string) error {
	_, err := db.Exec(ctx, `
		UPDATE rag_rollout_job
		SET status = CASE
			WHEN total_count > 0 THEN $2
			ELSE $3
		END,
		completed_at = CASE WHEN total_count = 0 THEN now() ELSE NULL END
		WHERE id = $1::uuid`,
		jobID, ragRolloutStatusRunning, ragRolloutStatusCompleted,
	)
	return err
}

func handleRAGRolloutMessage(ctx context.Context, msg *RAGRolloutMessage) error {
	if msg == nil || msg.TenantID == "" || msg.SchemaName == "" {
		return fmt.Errorf("invalid rollout message")
	}

	cancelled, err := ragRolloutJobCancelled(ctx, msg.JobID)
	if err != nil {
		return err
	}
	if cancelled {
		skipRAGRolloutItem(ctx, msg.ItemID, msg.JobID)
		return nil
	}

	if !msg.NotBefore.IsZero() && time.Now().Before(msg.NotBefore) {
		return rolloutTenantNotReady(msg.NotBefore)
	}

	var itemStatus string
	err = db.QueryRow(ctx, `
		SELECT status FROM rag_rollout_job_item WHERE id = $1::uuid`, msg.ItemID,
	).Scan(&itemStatus)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	switch itemStatus {
	case ragRolloutItemSucceeded, ragRolloutItemFailed, ragRolloutItemSkipped:
		return nil
	}

	result, err := db.Exec(ctx, `
		UPDATE rag_rollout_job_item
		SET status = $2, attempts = attempts + 1, updated_at = now()
		WHERE id = $1::uuid AND status = ANY($3::text[])`,
		msg.ItemID, ragRolloutItemRunning,
		[]string{ragRolloutItemQueued, ragRolloutItemRunning},
	)
	if err != nil {
		return err
	}
	affected := result.RowsAffected()
	if affected == 0 {
		return nil
	}

	if err := tenant.EnsureRetrievalSchema(ctx, msg.SchemaName); err != nil {
		return markRAGRolloutItemFailed(ctx, msg, err)
	}

	resp, err := SetTenantRetrievalMode(ctx, msg.TenantID, msg.Mode, true)
	if err != nil {
		return markRAGRolloutItemFailed(ctx, msg, err)
	}

	_, _ = db.Exec(ctx, `
		UPDATE rag_rollout_job_item
		SET status = $2, kb_enqueued = $3, catalog_enqueued = $4, error_text = NULL, updated_at = now()
		WHERE id = $1::uuid`,
		msg.ItemID, ragRolloutItemSucceeded, resp.KBEnqueued, resp.CatalogEnqueued,
	)
	_, _ = db.Exec(ctx, `
		UPDATE rag_rollout_job
		SET kb_enqueued_total = kb_enqueued_total + $2,
		    catalog_enqueued_total = catalog_enqueued_total + $3
		WHERE id = $1::uuid`,
		msg.JobID, resp.KBEnqueued, resp.CatalogEnqueued,
	)
	_ = incrementRAGRolloutJobCounter(ctx, msg.JobID, true)

	rlog.Info("rag rollout tenant done",
		"job", msg.JobID, "tenant", msg.TenantID, "mode", msg.Mode,
		"kb", resp.KBEnqueued, "catalog", resp.CatalogEnqueued)
	return finalizeRAGRolloutJobIfDone(ctx, msg.JobID)
}

func markRAGRolloutItemFailed(ctx context.Context, msg *RAGRolloutMessage, procErr error) error {
	_, _ = db.Exec(ctx, `
		UPDATE rag_rollout_job_item
		SET status = $2, error_text = $3, updated_at = now()
		WHERE id = $1::uuid`,
		msg.ItemID, ragRolloutItemFailed, procErr.Error(),
	)
	_ = incrementRAGRolloutJobCounter(ctx, msg.JobID, false)
	rlog.Warn("rag rollout tenant failed", "job", msg.JobID, "tenant", msg.TenantID, "err", procErr)
	_ = finalizeRAGRolloutJobIfDone(ctx, msg.JobID)
	if retrieval.IsRetryableError(procErr) {
		return procErr
	}
	return nil
}

func incrementRAGRolloutJobCounter(ctx context.Context, jobID string, success bool) error {
	if success {
		_, err := db.Exec(ctx, `
			UPDATE rag_rollout_job SET done_count = done_count + 1 WHERE id = $1::uuid`, jobID)
		return err
	}
	_, err := db.Exec(ctx, `
		UPDATE rag_rollout_job SET failed_count = failed_count + 1 WHERE id = $1::uuid`, jobID)
	return err
}

func rolloutTenantNotReady(notBefore time.Time) error {
	return fmt.Errorf("rollout tenant temporarily scheduled at %s", notBefore.UTC().Format(time.RFC3339))
}

func finalizeRAGRolloutJobIfDone(ctx context.Context, jobID string) error {
	var total, done, failed int
	err := db.QueryRow(ctx, `
		SELECT total_count, done_count, failed_count FROM rag_rollout_job WHERE id = $1::uuid`, jobID,
	).Scan(&total, &done, &failed)
	if err != nil {
		return err
	}
	if total > 0 && done+failed >= total {
		_, err = db.Exec(ctx, `
			UPDATE rag_rollout_job
			SET status = $2, completed_at = now()
			WHERE id = $1::uuid AND status = $3`,
			jobID, ragRolloutStatusCompleted, ragRolloutStatusRunning,
		)
	}
	return err
}

func ragRolloutJobCancelled(ctx context.Context, jobID string) (bool, error) {
	var status string
	err := db.QueryRow(ctx, `SELECT status FROM rag_rollout_job WHERE id = $1::uuid`, jobID).Scan(&status)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return status == ragRolloutStatusCancelled, nil
}

func skipRAGRolloutItem(ctx context.Context, itemID, jobID string) {
	if itemID == "" {
		return
	}
	res, err := db.Exec(ctx, `
		UPDATE rag_rollout_job_item
		SET status = $2, updated_at = now()
		WHERE id = $1::uuid AND status = ANY($3::text[])`,
		itemID, ragRolloutItemSkipped,
		[]string{ragRolloutItemQueued, ragRolloutItemRunning},
	)
	if err != nil {
		return
	}
	if n := res.RowsAffected(); n > 0 {
		_ = incrementRAGRolloutJobCounter(ctx, jobID, true)
		_ = finalizeRAGRolloutJobIfDone(ctx, jobID)
	}
}

func getRAGRolloutJob(ctx context.Context, jobID string) (*RAGRolloutJobSummary, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil, fmt.Errorf("jobId required")
	}
	var s RAGRolloutJobSummary
	var startedBy sql.NullString
	var completedAt sql.NullTime
	err := db.QueryRow(ctx, `
		SELECT id::text, mode, scope, status, total_count, done_count, failed_count,
		       kb_enqueued_total, catalog_enqueued_total, tenant_delay_ms,
		       started_by::text, created_at, completed_at
		FROM rag_rollout_job WHERE id = $1::uuid`, jobID,
	).Scan(
		&s.JobID, &s.Mode, &s.Scope, &s.Status, &s.TotalCount, &s.DoneCount, &s.FailedCount,
		&s.KBEnqueuedTotal, &s.CatalogEnqueuedTotal, &s.TenantDelayMs,
		&startedBy, &s.CreatedAt, &completedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("rollout job not found")
	}
	if err != nil {
		return nil, err
	}
	if startedBy.Valid {
		s.StartedBy = startedBy.String
	}
	if completedAt.Valid {
		t := completedAt.Time
		s.CompletedAt = &t
	}

	rows, err := db.Query(ctx, `
		SELECT COALESCE(error_text, schema_name)
		FROM rag_rollout_job_item
		WHERE job_id = $1::uuid AND status = $2
		ORDER BY updated_at DESC LIMIT 10`, jobID, ragRolloutItemFailed)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var line string
			if scanErr := rows.Scan(&line); scanErr == nil && line != "" {
				s.RecentErrors = append(s.RecentErrors, line)
			}
		}
	}
	return &s, nil
}

func listActiveRAGRolloutJobs(ctx context.Context) ([]RAGRolloutJobSummary, error) {
	rows, err := db.Query(ctx, `
		SELECT id::text, mode, scope, status, total_count, done_count, failed_count,
		       kb_enqueued_total, catalog_enqueued_total, tenant_delay_ms,
		       started_by::text, created_at, completed_at
		FROM rag_rollout_job
		WHERE status = ANY($1::text[])
		ORDER BY created_at DESC`,
		[]string{ragRolloutStatusPending, ragRolloutStatusRunning},
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []RAGRolloutJobSummary
	for rows.Next() {
		var s RAGRolloutJobSummary
		var startedBy sql.NullString
		var completedAt sql.NullTime
		if err := rows.Scan(
			&s.JobID, &s.Mode, &s.Scope, &s.Status, &s.TotalCount, &s.DoneCount, &s.FailedCount,
			&s.KBEnqueuedTotal, &s.CatalogEnqueuedTotal, &s.TenantDelayMs,
			&startedBy, &s.CreatedAt, &completedAt,
		); err != nil {
			return nil, err
		}
		if startedBy.Valid {
			s.StartedBy = startedBy.String
		}
		if completedAt.Valid {
			t := completedAt.Time
			s.CompletedAt = &t
		}
		jobs = append(jobs, s)
	}
	if jobs == nil {
		jobs = []RAGRolloutJobSummary{}
	}
	return jobs, rows.Err()
}

func cancelRAGRolloutJob(ctx context.Context, jobID string) (*RAGRolloutJobSummary, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil, fmt.Errorf("jobId required")
	}
	var status string
	err := db.QueryRow(ctx, `SELECT status FROM rag_rollout_job WHERE id = $1::uuid`, jobID).Scan(&status)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("rollout job not found")
	}
	if err != nil {
		return nil, err
	}
	if status != ragRolloutStatusPending && status != ragRolloutStatusRunning {
		return nil, fmt.Errorf("job tidak aktif (status: %s)", status)
	}

	_, _ = db.Exec(ctx, `
		UPDATE rag_rollout_job_item
		SET status = $2, error_text = 'dibatalkan admin', updated_at = now()
		WHERE job_id = $1::uuid AND status = ANY($3::text[])`,
		jobID, ragRolloutItemSkipped,
		[]string{ragRolloutItemQueued, ragRolloutItemRunning},
	)

	var total, done, failed int
	_ = db.QueryRow(ctx, `
		SELECT total_count,
		       (SELECT COUNT(*)::int FROM rag_rollout_job_item WHERE job_id = $1::uuid AND status = ANY($2::text[])),
		       (SELECT COUNT(*)::int FROM rag_rollout_job_item WHERE job_id = $1::uuid AND status = $3)
		FROM rag_rollout_job WHERE id = $1::uuid`,
		jobID,
		[]string{ragRolloutItemSucceeded, ragRolloutItemSkipped},
		ragRolloutItemFailed,
	).Scan(&total, &done, &failed)

	_, err = db.Exec(ctx, `
		UPDATE rag_rollout_job
		SET status = $2, done_count = $3, failed_count = $4, completed_at = now()
		WHERE id = $1::uuid`,
		jobID, ragRolloutStatusCancelled, done, failed,
	)
	if err != nil {
		return nil, err
	}
	return getRAGRolloutJob(ctx, jobID)
}

//encore:api auth method=POST path=/api/v1/flags/retrieval-rollout
func StartRAGRollout(ctx context.Context, req *StartRAGRolloutRequest) (*StartRAGRolloutResponse, error) {
	u, err := requireSuperAdmin()
	if err != nil {
		return nil, err
	}
	return enqueueRAGRollout(ctx, req, u.AccountID)
}

//encore:api auth method=GET path=/api/v1/flags/retrieval-rollout/jobs/:jobId
func GetRAGRolloutJob(ctx context.Context, jobId string) (*RAGRolloutJobSummary, error) {
	if _, err := requireSuperAdmin(); err != nil {
		return nil, err
	}
	return getRAGRolloutJob(ctx, jobId)
}

//encore:api auth method=GET path=/api/v1/flags/retrieval-rollout/active-jobs
func ListActiveRAGRolloutJobs(ctx context.Context) (*ListRAGRolloutJobsResponse, error) {
	if _, err := requireSuperAdmin(); err != nil {
		return nil, err
	}
	jobs, err := listActiveRAGRolloutJobs(ctx)
	if err != nil {
		return nil, err
	}
	return &ListRAGRolloutJobsResponse{Jobs: jobs}, nil
}

//encore:api auth method=POST path=/api/v1/flags/retrieval-rollout/jobs/:jobId/cancel
func CancelRAGRolloutJob(ctx context.Context, jobId string) (*RAGRolloutJobSummary, error) {
	if _, err := requireSuperAdmin(); err != nil {
		return nil, err
	}
	return cancelRAGRolloutJob(ctx, jobId)
}
