package kb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/storage/sqldb"

	e "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
)

var db = sqldb.Named("tenant")

func withTenantDB(ctx context.Context, schema string) (*sql.DB, error) {
	stdlib := db.Stdlib()
	_, err := stdlib.ExecContext(ctx, fmt.Sprintf(`SET search_path TO %q`, schema))
	if err != nil {
		return nil, fmt.Errorf("set search_path: %w", err)
	}
	return stdlib, nil
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
	return u, nil
}

func requireOwner(u *types.AuthUser) error {
	if u.Role != "owner" && u.Role != "super_admin" {
		return e.Forbidden("only owner can manage knowledge base")
	}
	return nil
}

// ─── Endpoints ───────────────────────────────────────────────────────────────

//encore:api auth method=GET path=/knowledge-base
func List(ctx context.Context, req *ListRequest) (*ListResponse, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}

	conn, err := withTenantDB(ctx, u.TenantSchema)
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

	countSQL := "SELECT COUNT(*) FROM knowledge_base_entries " + where
	var total int
	if err := conn.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count KB entries: %w", err)
	}

	querySQL := fmt.Sprintf(`
		SELECT id, question, answer, category, source, is_active,
		       created_at, updated_at, deleted_at, deleted_by
		FROM knowledge_base_entries %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, where, argN, argN+1)
	args = append(args, pageSize, offset)

	rows, err := conn.QueryContext(ctx, querySQL, args...)
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

//encore:api auth method=POST path=/knowledge-base
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

	conn, err := withTenantDB(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	var entry KBEntry
	err = conn.QueryRowContext(ctx, `
		INSERT INTO knowledge_base_entries (question, answer, category, source, is_active)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, question, answer, category, source, is_active, created_at, updated_at`,
		req.Question, req.Answer, req.Category, req.Source, isActive,
	).Scan(&entry.ID, &entry.Question, &entry.Answer, &entry.Category,
		&entry.Source, &entry.IsActive, &entry.CreatedAt, &entry.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create KB entry: %w", err)
	}
	return &CreateResponse{Entry: entry}, nil
}

//encore:api auth method=PATCH path=/knowledge-base/:id
func Update(ctx context.Context, id string, req *UpdateRequest) (*UpdateResponse, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}

	conn, err := withTenantDB(ctx, u.TenantSchema)
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

	query := fmt.Sprintf(`
		UPDATE knowledge_base_entries
		SET %s
		WHERE id = $%d AND deleted_at IS NULL
		RETURNING id, question, answer, category, source, is_active, created_at, updated_at`,
		joinStrings(sets, ", "), argN)

	var entry KBEntry
	err = conn.QueryRowContext(ctx, query, args...).Scan(
		&entry.ID, &entry.Question, &entry.Answer, &entry.Category,
		&entry.Source, &entry.IsActive, &entry.CreatedAt, &entry.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, e.NotFound("FAQ tidak ditemukan")
	}
	if err != nil {
		return nil, fmt.Errorf("update KB entry: %w", err)
	}
	return &UpdateResponse{Entry: entry}, nil
}

//encore:api auth method=DELETE path=/knowledge-base/:id
func Delete(ctx context.Context, id string) (*DeleteResponse, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}

	conn, err := withTenantDB(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}

	res, err := conn.ExecContext(ctx, `
		UPDATE knowledge_base_entries
		SET deleted_at = NOW(), deleted_by = $1
		WHERE id = $2 AND deleted_at IS NULL`,
		u.Email, id)
	if err != nil {
		return nil, fmt.Errorf("soft delete KB entry: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, e.NotFound("FAQ tidak ditemukan")
	}
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
