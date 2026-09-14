package admin

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"encore.dev/beta/errs"
	"encore.dev/rlog"

	"encore.app/wabantu/audit"
	"encore.app/wabantu/business"
	"encore.app/wabantu/kb"
	"encore.app/wabantu/order"
	"encore.app/wabantu/shared/triageincident"
	"encore.app/wabantu/shared/triagerepair"
	"encore.app/wabantu/system"
)

type AITriageRepairPlan struct {
	ID            string          `json:"id"`
	IncidentID    string          `json:"incidentId"`
	TenantID      string          `json:"tenantId"`
	TenantSchema  string          `json:"tenantSchema"`
	Operation     string          `json:"operation"`
	Status        string          `json:"status"`
	TargetOrderID string          `json:"targetOrderId,omitempty"`
	BlockReasons  []string        `json:"blockReasons,omitempty"`
	BeforeJSON    json.RawMessage `json:"beforeJson,omitempty"`
	AfterJSON     json.RawMessage `json:"afterJson,omitempty"`
	BeforeHash    string          `json:"beforeHash"`
	AfterHash     string          `json:"afterHash"`
	ApprovedBy    string          `json:"approvedBy,omitempty"`
	AppliedAt     *time.Time      `json:"appliedAt,omitempty"`
	ApplyResult   string          `json:"applyResult,omitempty"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}

type DryRunAITriageRepairParams struct {
	Operation     string            `json:"operation"`
	TargetOrderID string            `json:"targetOrderId,omitempty"`
	Items         []order.OrderItem `json:"items,omitempty"`
	CatalogItemID string            `json:"catalogItemId,omitempty"`
	KBEntryID     string            `json:"kbEntryId,omitempty"`
	Field         string            `json:"field,omitempty"`
	Value         string            `json:"value,omitempty"`
}

type DryRunAITriageRepairResponse struct {
	Plan AITriageRepairPlan `json:"plan"`
}

type GetAITriageRepairPlanResponse struct {
	Plan AITriageRepairPlan `json:"plan"`
}

// DryRunAITriageRepair creates a preview plan. Does not mutate tenant data.
//
//encore:api auth method=POST path=/api/v1/admin/ai-triage/incidents/:id/repair/dry-run tag:super_admin
func DryRunAITriageRepair(ctx context.Context, id string, p *DryRunAITriageRepairParams) (*DryRunAITriageRepairResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	if p == nil || !triagerepair.ValidOperation(p.Operation) {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "operation tidak diizinkan"}
	}
	inc, err := loadIncident(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	plan, err := buildRepairPlan(ctx, inc, p)
	if err != nil {
		return nil, err
	}
	saved, err := insertRepairPlan(ctx, plan)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "gagal menyimpan repair plan"}
	}
	_ = setIncidentRepairPlan(ctx, inc.ID, saved.ID)
	return &DryRunAITriageRepairResponse{Plan: saved}, nil
}

// GetAITriageRepairPlan returns one plan.
//
//encore:api auth method=GET path=/api/v1/admin/ai-triage/repair-plans/:id tag:super_admin
func GetAITriageRepairPlan(ctx context.Context, id string) (*GetAITriageRepairPlanResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	plan, err := loadRepairPlan(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	return &GetAITriageRepairPlanResponse{Plan: plan}, nil
}

// ApproveAITriageRepair marks a dry-run plan approved (separate from apply).
//
//encore:api auth method=POST path=/api/v1/admin/ai-triage/repair-plans/:id/approve tag:super_admin
func ApproveAITriageRepair(ctx context.Context, id string) (*GetAITriageRepairPlanResponse, error) {
	user, err := requireSuperAdmin(ctx)
	if err != nil {
		return nil, err
	}
	plan, err := loadRepairPlan(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if plan.Status == "approved" {
		return &GetAITriageRepairPlanResponse{Plan: plan}, nil
	}
	if plan.Status != "draft" {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "hanya plan draft yang bisa disetujui"}
	}
	if len(plan.BlockReasons) > 0 {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "plan terblokir"}
	}
	_, err = system.DB.Exec(ctx, `
		UPDATE ai_triage_repair_plan
		SET status = 'approved', approved_by = $2::uuid, approved_at = now(), updated_at = now()
		WHERE id = $1::uuid AND status = 'draft'`, id, user.AccountID)
	if err != nil {
		return nil, err
	}
	plan, _ = loadRepairPlan(ctx, id)
	return &GetAITriageRepairPlanResponse{Plan: plan}, nil
}

// ApplyAITriageRepair executes an approved plan (idempotent).
//
//encore:api auth method=POST path=/api/v1/admin/ai-triage/repair-plans/:id/apply tag:super_admin
func ApplyAITriageRepair(ctx context.Context, id string) (*GetAITriageRepairPlanResponse, error) {
	user, err := requireSuperAdmin(ctx)
	if err != nil {
		return nil, err
	}
	plan, err := loadRepairPlan(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if plan.Status == "applied" {
		return &GetAITriageRepairPlanResponse{Plan: plan}, nil
	}
	if plan.Status != "approved" && plan.Status != "applying" {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "apply hanya setelah persetujuan"}
	}
	_, _ = system.DB.Exec(ctx, `UPDATE ai_triage_repair_plan SET status = 'applying', updated_at = now() WHERE id = $1::uuid`, plan.ID)

	applyErr := executeRepair(ctx, plan)
	status := "applied"
	errText := ""
	if applyErr != nil {
		status = "failed"
		errText = applyErr.Error()
		rlog.Warn("repair apply failed", "planId", plan.ID, "err", applyErr)
	}
	_, err = system.DB.Exec(ctx, `
		UPDATE ai_triage_repair_plan
		SET status = $2, applied_by = $3::uuid, applied_at = now(), apply_result = $2,
		    error_text = NULLIF($4, ''), updated_at = now()
		WHERE id = $1::uuid`, plan.ID, status, user.AccountID, errText)
	if err != nil {
		return nil, err
	}
	changes, _ := json.Marshal(map[string]any{"planId": plan.ID, "operation": plan.Operation, "status": status})
	if recErr := audit.RecordAudit(ctx, &audit.RecordAuditParams{
		TenantID: plan.TenantID, UserID: user.AccountID, Action: "ai_triage_repair_apply",
		EntityType: "ai_triage_repair_plan", EntityID: plan.ID, Changes: changes,
	}); recErr != nil {
		rlog.Warn("repair audit failed", "planId", plan.ID, "err", recErr)
	}
	plan, _ = loadRepairPlan(ctx, plan.ID)
	return &GetAITriageRepairPlanResponse{Plan: plan}, nil
}

func buildRepairPlan(ctx context.Context, inc triageincident.Incident, p *DryRunAITriageRepairParams) (AITriageRepairPlan, error) {
	plan := AITriageRepairPlan{
		IncidentID:    inc.ID,
		TenantID:      inc.TenantID,
		TenantSchema:  inc.TenantSchema,
		Operation:     p.Operation,
		Status:        "draft",
		TargetOrderID: strings.TrimSpace(p.TargetOrderID),
	}
	switch p.Operation {
	case triagerepair.OpDraftItemsReplace, triagerepair.OpDraftItemsMerge:
		if plan.TargetOrderID == "" {
			plan.Status = "blocked"
			plan.BlockReasons = []string{triagerepair.BlockCartNotPersisted}
			return plan, nil
		}
		res, err := order.DryRunTriageDraftRepair(ctx, &order.TriageDraftRepairParams{
			TenantSchema:   inc.TenantSchema,
			OrderID:        plan.TargetOrderID,
			ConversationID: threadRefFromEvidence(inc.Evidence),
			Operation:      p.Operation,
			Items:          p.Items,
		})
		if err != nil {
			return plan, err
		}
		plan.BeforeJSON, plan.AfterJSON = res.BeforeJSON, res.AfterJSON
		plan.BeforeHash, plan.AfterHash = res.BeforeHash, res.AfterHash
		plan.BlockReasons = res.BlockReasons
		if res.Blocked {
			plan.Status = "blocked"
		}
	case triagerepair.OpKBAnswerPatch:
		res, err := kb.ApplyTriageKBPatch(ctx, &kb.TriageKBPatchParams{
			TenantID: inc.TenantID, TenantSchema: inc.TenantSchema, EntryID: p.KBEntryID,
			Field: p.Field, Value: p.Value, Apply: false,
		})
		if err != nil {
			return plan, err
		}
		plan.BeforeJSON, _ = json.Marshal(res.Before)
		plan.AfterJSON, _ = json.Marshal(res.After)
		plan.BeforeHash, plan.AfterHash = res.BeforeHash, res.AfterHash
	case triagerepair.OpCatalogFieldPatch:
		res, err := business.ApplyTriageCatalogPatch(ctx, &business.TriageCatalogPatchParams{
			TenantSchema: inc.TenantSchema, ItemID: p.CatalogItemID, Field: p.Field, Value: p.Value, Apply: false,
		})
		if err != nil {
			return plan, err
		}
		plan.BeforeJSON, _ = json.Marshal(res.Before)
		plan.AfterJSON, _ = json.Marshal(res.After)
		plan.BeforeHash, plan.AfterHash = res.BeforeHash, res.AfterHash
	default:
		plan.Status = "blocked"
		plan.BlockReasons = []string{"operation_not_implemented"}
	}
	return plan, nil
}

func executeRepair(ctx context.Context, plan AITriageRepairPlan) error {
	switch plan.Operation {
	case triagerepair.OpDraftItemsReplace, triagerepair.OpDraftItemsMerge:
		var items []order.OrderItem
		_ = json.Unmarshal(plan.AfterJSON, &items)
		res, err := order.ApplyTriageDraftRepair(ctx, &order.TriageDraftRepairParams{
			TenantSchema: plan.TenantSchema, OrderID: plan.TargetOrderID,
			ConversationID: threadRefFromEvidence(nil),
			Operation:      plan.Operation, Items: items, ExpectedHash: plan.BeforeHash, Apply: true,
		})
		if err != nil {
			return err
		}
		if res.Blocked {
			return &errs.Error{Code: errs.FailedPrecondition, Message: strings.Join(res.BlockReasons, ",")}
		}
		return nil
	case triagerepair.OpKBAnswerPatch:
		return nil
	default:
		return nil
	}
}

func threadRefFromEvidence(raw json.RawMessage) string {
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if v, ok := m["threadRef"].(string); ok {
		return v
	}
	return ""
}

func insertRepairPlan(ctx context.Context, plan AITriageRepairPlan) (AITriageRepairPlan, error) {
	var id string
	err := system.DB.QueryRow(ctx, `
		INSERT INTO ai_triage_repair_plan (
			incident_id, tenant_id, tenant_schema, operation, status, target_order_id,
			block_reasons, before_json, after_json, before_hash, after_hash
		) VALUES (
			$1::uuid, $2::uuid, $3, $4, $5, NULLIF($6, '')::uuid,
			string_to_array($7, ','), $8::jsonb, $9::jsonb, $10, $11
		) RETURNING id::text`,
		plan.IncidentID, plan.TenantID, plan.TenantSchema, plan.Operation, plan.Status, plan.TargetOrderID,
		strings.Join(plan.BlockReasons, ","), nullJSON(plan.BeforeJSON), nullJSON(plan.AfterJSON),
		plan.BeforeHash, plan.AfterHash,
	).Scan(&id)
	if err != nil {
		return plan, err
	}
	return loadRepairPlan(ctx, id)
}

func loadRepairPlan(ctx context.Context, id string) (AITriageRepairPlan, error) {
	var p AITriageRepairPlan
	parsed, err := requireTriageUUID(id)
	if err != nil {
		return p, err
	}
	id = parsed
	var orderID, approved, reasons sql.NullString
	var applied sql.NullTime
	err = system.DB.QueryRow(ctx, `
		SELECT id::text, incident_id::text, tenant_id::text, tenant_schema, operation, status,
		       target_order_id::text, array_to_string(block_reasons, ','), before_json, after_json,
		       before_hash, after_hash, approved_by::text, applied_at, apply_result, created_at, updated_at
		FROM ai_triage_repair_plan WHERE id = $1::uuid`, id).Scan(
		&p.ID, &p.IncidentID, &p.TenantID, &p.TenantSchema, &p.Operation, &p.Status,
		&orderID, &reasons, &p.BeforeJSON, &p.AfterJSON,
		&p.BeforeHash, &p.AfterHash, &approved, &applied, &p.ApplyResult, &p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return p, &errs.Error{Code: errs.NotFound, Message: "repair plan tidak ditemukan"}
	}
	if err != nil {
		return p, err
	}
	p.TargetOrderID = orderID.String
	p.ApprovedBy = approved.String
	if reasons.String != "" {
		p.BlockReasons = strings.Split(reasons.String, ",")
	}
	if applied.Valid {
		p.AppliedAt = &applied.Time
	}
	return p, nil
}

func contentHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
