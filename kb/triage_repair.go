package kb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	e "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/retrieval"
	"encore.app/wabantu/shared/triagerepair"
)

type TriageKBPatchParams struct {
	TenantID     string `json:"tenantId"`
	TenantSchema string `json:"tenantSchema"`
	EntryID      string `json:"entryId"`
	Field        string `json:"field"`
	Value        string `json:"value"`
	Apply        bool   `json:"apply"`
}

type TriageKBPatchResult struct {
	Blocked    bool   `json:"blocked"`
	BeforeHash string `json:"beforeHash"`
	AfterHash  string `json:"afterHash"`
	Before     string `json:"before"`
	After      string `json:"after"`
}

// ApplyTriageKBPatch patches an allowlisted KB field and enqueues reindex (private).
//
//encore:api private method=POST path=/api/v1/internal/kb/triage-patch
func ApplyTriageKBPatch(ctx context.Context, p *TriageKBPatchParams) (*TriageKBPatchResult, error) {
	if p == nil || strings.TrimSpace(p.TenantSchema) == "" || strings.TrimSpace(p.EntryID) == "" {
		return nil, e.BadRequest("tenantSchema and entryId required")
	}
	if !triagerepair.AllowedKBFields[p.Field] {
		return nil, e.BadRequest("field KB tidak diizinkan")
	}
	ts, err := openTenantScope(ctx, p.TenantSchema)
	if err != nil {
		return nil, err
	}
	var question, answer string
	var active bool
	err = ts.QueryRowContext(ctx, `
		SELECT question, answer, is_active FROM knowledge_base_entry
		WHERE id = $1::uuid AND deleted_at IS NULL`, p.EntryID).Scan(&question, &answer, &active)
	if err == sql.ErrNoRows {
		return nil, e.NotFound("FAQ tidak ditemukan")
	}
	if err != nil {
		return nil, err
	}
	afterQ, afterA, afterActive := question, answer, active
	switch p.Field {
	case "question":
		afterQ = strings.TrimSpace(p.Value)
	case "answer":
		afterA = strings.TrimSpace(p.Value)
	case "is_active":
		afterActive = strings.EqualFold(p.Value, "true") || p.Value == "1"
	}
	res := &TriageKBPatchResult{
		Before:     question + "\n" + answer,
		After:      afterQ + "\n" + afterA,
		BeforeHash: hashText(question + "\n" + answer),
		AfterHash:  hashText(afterQ + "\n" + afterA),
	}
	if !p.Apply {
		return res, nil
	}

	tx, err := ts.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	tTx := txn(ts, tx)
	var entry KBEntry
	err = tTx.QueryRowContext(ctx, `
		UPDATE knowledge_base_entry
		SET question = $2, answer = $3, is_active = $4, updated_at = NOW()
		WHERE id = $1::uuid AND deleted_at IS NULL
		RETURNING id, question, answer, category, source, is_active, created_at, updated_at`,
		p.EntryID, afterQ, afterA, afterActive,
	).Scan(&entry.ID, &entry.Question, &entry.Answer, &entry.Category, &entry.Source, &entry.IsActive, &entry.CreatedAt, &entry.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("update KB entry: %w", err)
	}
	version, err := bumpKBEmbeddingPendingTx(ctx, tTx, entry.ID, entry.Question, entry.Answer)
	if err != nil {
		return nil, err
	}
	hash := kbContentHash(entry.Question, entry.Answer)
	eventType := outboxEventIndexKB
	if !entry.IsActive {
		eventType = outboxEventDeleteKB
	}
	var outboxID string
	err = tTx.QueryRowContext(ctx, `
		INSERT INTO retrieval_outbox (event_type, entity_type, entity_id, version, content_hash, status)
		VALUES ($1, $2, $3::uuid, $4, $5, 'pending')
		RETURNING id::text`,
		eventType, entityTypeKB, entry.ID, version, hash,
	).Scan(&outboxID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if p.TenantID != "" {
		invalidateFAQCacheAfterKBChange(ctx, p.TenantID)
		publishKBIndexAfterCommit(ctx, p.TenantSchema, p.TenantID, outboxID, entry.ID, eventType, version, retrieval.IndexLaneLive)
	}
	return res, nil
}

func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
