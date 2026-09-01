package kb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/storage/sqldb"

	appdb "encore.app/wabantu/shared/db"
	e "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/retrieval"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/tenant"
)

var db = sqldb.Named("tenant")

func openTenantScope(ctx context.Context, schema string) (appdb.TenantScope, error) {
	if err := tenant.PrepareTenantAccess(ctx, schema); err != nil {
		return appdb.TenantScope{}, e.Internal(err.Error())
	}
	if err := tenant.EnsureKnowledgeBaseSchema(ctx, schema); err != nil {
		return appdb.TenantScope{}, e.Internal(err.Error())
	}
	if err := tenant.EnsureRetrievalSchema(ctx, schema); err != nil {
		return appdb.TenantScope{}, e.Internal(err.Error())
	}
	return appdb.OpenTenantScope(db.Stdlib(), schema), nil
}

// ─── Types ───────────────────────────────────────────────────────────────────

type KBEntry struct {
	ID        string     `json:"id"`
	Question  string     `json:"question"`
	Answer    string     `json:"answer"`
	Category  *string    `json:"category,omitempty"`
	Source    *string    `json:"source,omitempty"`
	IsActive  bool       `json:"isActive"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	DeletedAt *time.Time `json:"deletedAt,omitempty"`
	DeletedBy *string    `json:"deletedBy,omitempty"`
}

type ListRequest struct {
	Search   string `query:"search"`
	Category string `query:"category"`
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
}

type ListResponse struct {
	Items    []KBEntry `json:"items"`
	Total    int       `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"pageSize"`
}

type CreateRequest struct {
	Question string  `json:"question"`
	Answer   string  `json:"answer"`
	Category *string `json:"category,omitempty"`
	Source   *string `json:"source,omitempty"`
	IsActive *bool   `json:"isActive,omitempty"`
}

type CreateResponse struct {
	Entry KBEntry `json:"entry"`
}

type UpdateRequest struct {
	ID       string  `json:"-"`
	Question *string `json:"question,omitempty"`
	Answer   *string `json:"answer,omitempty"`
	Category *string `json:"category,omitempty"`
	Source   *string `json:"source,omitempty"`
	IsActive *bool   `json:"isActive,omitempty"`
}

type UpdateResponse struct {
	Entry KBEntry `json:"entry"`
}

type DeleteRequest struct {
	ID string `json:"-"`
}

type DeleteResponse struct {
	OK bool `json:"ok"`
}

// ─── Auth helper ─────────────────────────────────────────────────────────────

func currentUser(ctx context.Context) (*types.AuthUser, error) {
	uid, authed := auth.UserID()
	data := auth.Data()
	if !authed || uid == "" || data == nil {
		return nil, e.Unauthenticated("not authenticated")
	}
	u, valid := data.(*types.AuthUser)
	if !valid {
		return nil, e.Unauthenticated("invalid auth data")
	}
	if !u.HasEffectiveTenantContext() {
		return nil, e.Forbidden("tenant context required")
	}
	if err := u.RequireModule("ai"); err != nil {
		return nil, err
	}
	return u, nil
}

func requireOwner(u *types.AuthUser) error {
	if !u.CanPerformOwnerActions() {
		return e.Forbidden("only owner can manage knowledge base")
	}
	return nil
}

func resolveKBEntrySource(src *string) string {
	if src == nil {
		return "manual"
	}
	s := strings.TrimSpace(*src)
	if s == "" {
		return "manual"
	}
	return s
}

// ─── Endpoints ───────────────────────────────────────────────────────────────

//encore:api auth method=GET path=/api/v1/knowledge-base
func List(ctx context.Context, req *ListRequest) (*ListResponse, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}

	ts, err := openTenantScope(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}

	page := req.Page
	if page < 1 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	offset := (page - 1) * pageSize

	where := "WHERE deleted_at IS NULL"
	var args []any
	argN := 1

	if cat := strings.TrimSpace(req.Category); cat != "" {
		where += fmt.Sprintf(" AND category = $%d", argN)
		args = append(args, cat)
		argN++
	}
	if search := strings.TrimSpace(req.Search); search != "" {
		where += fmt.Sprintf(" AND question ILIKE $%d", argN)
		args = append(args, "%"+search+"%")
		argN++
	}

	countSQL := "SELECT COUNT(*) FROM knowledge_base_entry " + where
	var total int
	if err := ts.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count KB entries: %w", err)
	}

	querySQL := fmt.Sprintf(`
		SELECT id, question, answer, category, source, is_active,
		       created_at, updated_at, deleted_at, deleted_by
		FROM knowledge_base_entry %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, where, argN, argN+1)
	args = append(args, pageSize, offset)

	rows, err := ts.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("list KB entries: %w", err)
	}
	defer rows.Close()

	var items []KBEntry
	for rows.Next() {
		var entry KBEntry
		if err := rows.Scan(
			&entry.ID, &entry.Question, &entry.Answer, &entry.Category,
			&entry.Source, &entry.IsActive, &entry.CreatedAt, &entry.UpdatedAt,
			&entry.DeletedAt, &entry.DeletedBy,
		); err != nil {
			return nil, err
		}
		items = append(items, entry)
	}
	if items == nil {
		items = []KBEntry{}
	}
	return &ListResponse{Items: items, Total: total, Page: page, PageSize: pageSize}, rows.Err()
}

//encore:api auth method=POST path=/api/v1/knowledge-base
func Create(ctx context.Context, req *CreateRequest) (*CreateResponse, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}
	if req.Question == "" || req.Answer == "" {
		return nil, e.BadRequest("question and answer are required")
	}

	ts, err := openTenantScope(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	source := resolveKBEntrySource(req.Source)

	tx, err := ts.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	tTx := txn(ts, tx)

	var entry KBEntry
	var version int64
	err = tTx.QueryRowContext(ctx, `
		INSERT INTO knowledge_base_entry (question, answer, category, source, is_active,
		    embedding_version, embedding_status, embedding_content_hash, embedding_model, embedding_updated_at)
		VALUES ($1, $2, $3, $4, $5, 1, 'pending', $6, $7, NOW())
		RETURNING id, question, answer, category, source, is_active, created_at, updated_at, embedding_version`,
		req.Question, req.Answer, req.Category, source, isActive,
		kbContentHash(req.Question, req.Answer), retrieval.EmbeddingModel,
	).Scan(&entry.ID, &entry.Question, &entry.Answer, &entry.Category,
		&entry.Source, &entry.IsActive, &entry.CreatedAt, &entry.UpdatedAt, &version)
	if err != nil {
		return nil, fmt.Errorf("create KB entry: %w", err)
	}

	var outboxID string
	err = tTx.QueryRowContext(ctx, `
		INSERT INTO retrieval_outbox (event_type, entity_type, entity_id, version, content_hash, status)
		VALUES ($1, $2, $3::uuid, $4, $5, 'pending')
		RETURNING id::text`,
		outboxEventIndexKB, entityTypeKB, entry.ID, version, kbContentHash(req.Question, req.Answer),
	).Scan(&outboxID)
	if err != nil {
		return nil, fmt.Errorf("enqueue KB outbox: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	publishKBIndexAfterCommit(ctx, u.TenantSchema, u.TenantID, outboxID, entry.ID, outboxEventIndexKB, version, retrieval.IndexLaneLive)
	return &CreateResponse{Entry: entry}, nil
}

//encore:api auth method=PATCH path=/api/v1/knowledge-base/:id
func Update(ctx context.Context, id string, req *UpdateRequest) (*UpdateResponse, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}

	ts, err := openTenantScope(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}

	sets := []string{}
	args := []any{}
	argN := 1

	if req.Question != nil {
		sets = append(sets, fmt.Sprintf("question = $%d", argN))
		args = append(args, *req.Question)
		argN++
	}
	if req.Answer != nil {
		sets = append(sets, fmt.Sprintf("answer = $%d", argN))
		args = append(args, *req.Answer)
		argN++
	}
	if req.Category != nil {
		sets = append(sets, fmt.Sprintf("category = $%d", argN))
		args = append(args, *req.Category)
		argN++
	}
	if req.Source != nil {
		sets = append(sets, fmt.Sprintf("source = $%d", argN))
		args = append(args, *req.Source)
		argN++
	}
	if req.IsActive != nil {
		sets = append(sets, fmt.Sprintf("is_active = $%d", argN))
		args = append(args, *req.IsActive)
		argN++
	}

	if len(sets) == 0 {
		return nil, e.BadRequest("no fields to update")
	}

	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)

	tx, err := ts.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	tTx := txn(ts, tx)

	query := fmt.Sprintf(`
		UPDATE knowledge_base_entry
		SET %s
		WHERE id = $%d AND deleted_at IS NULL
		RETURNING id, question, answer, category, source, is_active, created_at, updated_at`,
		joinStrings(sets, ", "), argN)

	var entry KBEntry
	err = tTx.QueryRowContext(ctx, query, args...).Scan(
		&entry.ID, &entry.Question, &entry.Answer, &entry.Category,
		&entry.Source, &entry.IsActive, &entry.CreatedAt, &entry.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, e.NotFound("FAQ tidak ditemukan")
	}
	if err != nil {
		return nil, fmt.Errorf("update KB entry: %w", err)
	}

	version, err := bumpKBEmbeddingPendingTx(ctx, tTx, entry.ID, entry.Question, entry.Answer)
	if err != nil {
		return nil, fmt.Errorf("bump embedding version: %w", err)
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
	publishKBIndexAfterCommit(ctx, u.TenantSchema, u.TenantID, outboxID, entry.ID, eventType, version, retrieval.IndexLaneLive)
	return &UpdateResponse{Entry: entry}, nil
}

//encore:api auth method=DELETE path=/api/v1/knowledge-base/:id
func Delete(ctx context.Context, id string) (*DeleteResponse, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}

	ts, err := openTenantScope(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}

	tx, err := ts.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	tTx := txn(ts, tx)

	var version int64
	err = tTx.QueryRowContext(ctx, `
		UPDATE knowledge_base_entry
		SET deleted_at = NOW(), deleted_by = $1,
		    embedding_status = 'pending', embedding_updated_at = NOW()
		WHERE id = $2::uuid AND deleted_at IS NULL
		RETURNING embedding_version`, u.Email, id).Scan(&version)
	if err == sql.ErrNoRows {
		return nil, e.NotFound("FAQ tidak ditemukan")
	}
	if err != nil {
		return nil, fmt.Errorf("soft delete KB entry: %w", err)
	}

	var outboxID string
	err = tTx.QueryRowContext(ctx, `
		INSERT INTO retrieval_outbox (event_type, entity_type, entity_id, version, status)
		VALUES ($1, $2, $3::uuid, $4, 'pending')
		RETURNING id::text`,
		outboxEventDeleteKB, entityTypeKB, id, version,
	).Scan(&outboxID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	publishKBIndexAfterCommit(ctx, u.TenantSchema, u.TenantID, outboxID, id, outboxEventDeleteKB, version, retrieval.IndexLaneLive)
	return &DeleteResponse{OK: true}, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += sep + p
	}
	return result
}
