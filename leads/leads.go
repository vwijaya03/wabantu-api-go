package leads

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"

	appdb "encore.app/wabantu/shared/db"
	e "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/pii"
	"encore.app/wabantu/shared/entitlement"
	"encore.app/wabantu/shared/tenantschema"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/usage"
)

var validLeadStatuses = map[string]bool{
	"new": true, "interested": true, "negotiation": true, "paid": true, "lost": true,
}

var db = sqldb.Named("tenant")

func tenantConn(ctx context.Context, schema string) (*sql.Conn, error) {
	return appdb.TenantConn(ctx, db.Stdlib(), schema)
}

// Lead matches the NestJS / web-frontend contract.
type Lead struct {
	ID              string    `json:"id"`
	PhoneNumber     string    `json:"phoneNumber"`
	Name            *string   `json:"name"`
	ProductInterest *string   `json:"productInterest"`
	Budget          *string   `json:"budget"`
	Location        *string   `json:"location"`
	Status          string    `json:"status"`
	Notes           *string   `json:"notes"`
	CreatedAt       time.Time `json:"createdAt"`
}

type ListRequest struct {
	Status string `query:"status"`
}

// ListLeadsResponse wraps the lead list (Encore requires a named struct).
type ListLeadsResponse struct {
	Items []Lead `json:"items"`
}

type UpdateRequest struct {
	Status *string `json:"status"`
	Notes  *string `json:"notes"`
}

type CaptureRequest struct {
	TenantSchema   string `json:"tenantSchema"`
	ContactID      string `json:"contactId"`
	ConversationID string `json:"conversationId"`
	ContactName    string `json:"contactName"`
	PhoneNumber    string `json:"phoneNumber"`
	Body           string `json:"body"`
}

type CaptureResponse struct {
	Created bool   `json:"created"`
	LeadID  string `json:"leadId"`
}

func currentUser(ctx context.Context) (*types.AuthUser, error) {
	uid, ok := auth.UserID()
	data := auth.Data()
	if !ok || uid == "" || data == nil {
		return nil, e.Unauthenticated("not authenticated")
	}
	u, ok := data.(*types.AuthUser)
	if !ok {
		return nil, e.Unauthenticated("invalid auth data")
	}
	return u, nil
}

// List returns all leads for the tenant (optional status filter).
//
//encore:api auth method=GET path=/api/v1/leads
func List(ctx context.Context, req *ListRequest) (*ListLeadsResponse, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if !entitlement.HasFeature(usage.TenantPlan(ctx, u.TenantSchema), entitlement.FeatureCRMLeads) {
		return nil, e.Forbidden("CRM leads requires Business plan or higher")
	}

	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	defer appdb.CloseTenantConn(conn)

	where := "WHERE l.deleted_at IS NULL"
	var args []any
	if status := strings.TrimSpace(req.Status); status != "" {
		where += " AND l.status = $1"
		args = append(args, status)
	}

	piiActive, _ := tenantschema.LeadPIIActive(ctx, conn, u.TenantSchema)
	var querySQL string
	if piiActive {
		querySQL = fmt.Sprintf(`
		SELECT l.id,
		       COALESCE(l.phone_number_enc,''), COALESCE(l.phone_number,''),
		       COALESCE(l.name_enc,''), COALESCE(l.name,''),
		       l.product_interest, l.budget, l.location,
		       l.status, l.notes, l.created_at
		FROM lead l
		%s
		ORDER BY l.created_at DESC`, where)
	} else {
		querySQL = fmt.Sprintf(`
		SELECT l.id,
		       '', COALESCE(l.phone_number,''),
		       '', COALESCE(l.name,''),
		       l.product_interest, l.budget, l.location,
		       l.status, l.notes, l.created_at
		FROM lead l
		%s
		ORDER BY l.created_at DESC`, where)
	}

	rows, err := conn.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("list leads: %w", err)
	}
	defer rows.Close()

	var items []Lead
	for rows.Next() {
		l, err := scanLeadPII(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, l)
	}
	if items == nil {
		items = []Lead{}
	}
	return &ListLeadsResponse{Items: items}, rows.Err()
}

// Update patches lead status and/or notes.
//
//encore:api auth method=PATCH path=/api/v1/leads/:id
func Update(ctx context.Context, id string, req *UpdateRequest) (*Lead, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if !entitlement.HasFeature(usage.TenantPlan(ctx, u.TenantSchema), entitlement.FeatureCRMLeads) {
		return nil, e.Forbidden("CRM leads requires Business plan or higher")
	}
	if req == nil || (req.Status == nil && req.Notes == nil) {
		return nil, e.BadRequest("nothing to update")
	}

	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	defer appdb.CloseTenantConn(conn)

	sets := make([]string, 0, 2)
	args := make([]any, 0, 3)
	n := 1
	if req.Status != nil {
		st := strings.ToLower(strings.TrimSpace(*req.Status))
		if !validLeadStatuses[st] {
			return nil, e.BadRequest("status must be one of: new, interested, negotiation, paid, lost")
		}
		sets = append(sets, fmt.Sprintf("status = $%d", n))
		args = append(args, st)
		n++
	}
	if req.Notes != nil {
		sets = append(sets, fmt.Sprintf("notes = $%d", n))
		args = append(args, req.Notes)
		n++
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE lead SET %s
		WHERE id = $%d AND deleted_at IS NULL`,
		strings.Join(sets, ", "), n)

	if _, err = conn.ExecContext(ctx, query, args...); err != nil {
		return nil, fmt.Errorf("update lead: %w", err)
	}
	var l Lead
	l, err = scanLeadPII(conn.QueryRowContext(ctx, `
		SELECT id,
		       COALESCE(phone_number_enc,''), COALESCE(phone_number,''),
		       COALESCE(name_enc,''), COALESCE(name,''),
		       product_interest, budget, location, status, notes, created_at
		FROM lead WHERE id = $1 AND deleted_at IS NULL`, id))
	if err == sql.ErrNoRows {
		return nil, e.NotFound("Lead tidak ditemukan")
	}
	if err != nil {
		return nil, fmt.Errorf("update lead: %w", err)
	}
	return &l, nil
}

// CaptureFromMessage auto-creates or updates a lead from an inbound message.
//
//encore:api private method=POST path=/api/v1/leads/capture
func CaptureFromMessage(ctx context.Context, req *CaptureRequest) (*CaptureResponse, error) {
	if req.TenantSchema == "" || req.ContactID == "" || req.ConversationID == "" {
		return nil, e.BadRequest("tenantSchema, contactId, conversationId required")
	}

	conn, err := tenantConn(ctx, req.TenantSchema)
	if err != nil {
		return nil, err
	}
	defer appdb.CloseTenantConn(conn)

	text := strings.TrimSpace(req.Body)
	if text == "" {
		return &CaptureResponse{Created: false}, nil
	}

	lower := strings.ToLower(text)
	signals := []string{"harga", "order", "pesan", "stok", "budget", "lokasi", "kirim", "cod", "minat"}
	hasSignal := false
	for _, k := range signals {
		if strings.Contains(lower, k) {
			hasSignal = true
			break
		}
	}
	if !hasSignal {
		return &CaptureResponse{Created: false}, nil
	}

	var existingID string
	err = conn.QueryRowContext(ctx, `
		SELECT id FROM lead
		WHERE conversation_id = $1 AND status = 'new' AND deleted_at IS NULL
		ORDER BY created_at DESC LIMIT 1`,
		req.ConversationID,
	).Scan(&existingID)
	if err == nil {
		if req.ContactName != "" {
			usePII, _ := tenantschema.LeadPIIActive(ctx, conn, req.TenantSchema)
			if usePII {
				nameEnc, nameIdx, encErr := encryptLeadName(req.ContactName)
				if encErr == nil {
					_, _ = conn.ExecContext(ctx, `
						UPDATE lead SET name_enc = $1, name_idx = $2, name = $3, updated_at = NOW()
						WHERE id = $4`, nameEnc, nameIdx, pii.Placeholder, existingID)
				}
			} else {
				_, _ = conn.ExecContext(ctx, `
					UPDATE lead SET name = $1, updated_at = NOW()
					WHERE id = $2 AND (name IS NULL OR TRIM(name) = '')`, req.ContactName, existingID)
			}
		}
		return &CaptureResponse{Created: false, LeadID: existingID}, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("check existing lead: %w", err)
	}

	phone := req.PhoneNumber
	if phone == "" {
		phone, _ = leadPhoneFromContact(ctx, conn, req.ContactID)
	}
	usePII, _ := tenantschema.LeadPIIActive(ctx, conn, req.TenantSchema)
	var newID string
	if usePII && pii.ValidateKey(leadEncKey()) == nil {
		phoneEnc, phoneIdx, encErr := encryptLeadPhone(phone)
		if encErr != nil {
			return nil, fmt.Errorf("encrypt lead phone: %w", encErr)
		}
		var nameEnc, nameIdx string
		if n := strings.TrimSpace(req.ContactName); n != "" {
			nameEnc, nameIdx, encErr = encryptLeadName(n)
			if encErr != nil {
				return nil, fmt.Errorf("encrypt lead name: %w", encErr)
			}
		}
		err = conn.QueryRowContext(ctx, `
			INSERT INTO lead (contact_id, conversation_id, phone_number, phone_number_enc, phone_number_idx, name, name_enc, name_idx, status, metadata)
			VALUES ($1, $2, $3, $4, $5, $3, $6, $7, 'new', $8::jsonb)
			RETURNING id`,
			req.ContactID, req.ConversationID, pii.Placeholder, phoneEnc, phoneIdx, nameEnc, nameIdx,
			fmt.Sprintf(`{"source":"webhook","triggerMessage":%q}`, text),
		).Scan(&newID)
	} else {
		var name *string
		if n := strings.TrimSpace(req.ContactName); n != "" {
			name = &n
		}
		err = conn.QueryRowContext(ctx, `
			INSERT INTO lead (contact_id, conversation_id, phone_number, name, status, metadata)
			VALUES ($1, $2, $3, $4, 'new', $5::jsonb)
			RETURNING id`,
			req.ContactID, req.ConversationID, phone, name,
			fmt.Sprintf(`{"source":"webhook","triggerMessage":%q}`, text),
		).Scan(&newID)
	}
	if err != nil {
		return nil, fmt.Errorf("insert lead: %w", err)
	}

	rlog.Info("lead captured from message", "leadId", newID, "contactId", req.ContactID)
	return &CaptureResponse{Created: true, LeadID: newID}, nil
}
