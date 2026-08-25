package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"

	appdb "encore.app/wabantu/shared/db"
	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/entitlement"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/tenant"
	"encore.app/wabantu/usage"
)

var db = sqldb.Named("tenant")

func openTenantScope(ctx context.Context, schema string) (appdb.TenantScope, error) {
	if err := tenant.PrepareTenantAccess(ctx, schema); err != nil {
		return appdb.TenantScope{}, err
	}
	return appdb.OpenTenantScope(db.Stdlib(), schema), nil
}

type Rule struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	TriggerType   string          `json:"triggerType"`
	TriggerValue  string          `json:"triggerValue"`
	ActionType    string          `json:"actionType"`
	ActionPayload json.RawMessage `json:"actionPayload"`
	BranchID      *string         `json:"branchId,omitempty"`
	IsActive      bool            `json:"isActive"`
	Priority      int             `json:"priority"`
	CreatedAt     time.Time       `json:"createdAt"`
}

type ListRulesResponse struct {
	Rules []Rule `json:"rules"`
}

type CreateRuleRequest struct {
	Name          string          `json:"name"`
	TriggerType   string          `json:"triggerType"`
	TriggerValue  string          `json:"triggerValue"`
	ActionType    string          `json:"actionType"`
	ActionPayload json.RawMessage `json:"actionPayload"`
	BranchID      *string         `json:"branchId,omitempty"`
	Priority      int             `json:"priority"`
}

type CreateRuleResponse struct {
	Rule Rule `json:"rule"`
}

type UpdateRuleRequest struct {
	Name          string          `json:"name"`
	TriggerType   string          `json:"triggerType"`
	TriggerValue  string          `json:"triggerValue"`
	ActionType    string          `json:"actionType"`
	ActionPayload json.RawMessage `json:"actionPayload"`
	Priority      int             `json:"priority"`
	IsActive      *bool           `json:"isActive,omitempty"`
}

type UpdateRuleResponse struct {
	Rule Rule `json:"rule"`
}

//encore:api auth method=GET path=/api/v1/workflows
func ListRules(ctx context.Context) (*ListRulesResponse, error) {
	u, err := user(ctx)
	if err != nil {
		return nil, err
	}
	if err := entitlement.Require(ctx, u.TenantSchema, entitlement.FeatureWorkflow); err != nil {
		return nil, err
	}
	ts, err := openTenantScope(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal("database connection failed")
	}

	rows, err := ts.QueryContext(ctx, `
		SELECT id, name, trigger_type, trigger_value, action_type, action_payload,
		       branch_id, is_active, priority, created_at
		FROM workflow_rule WHERE deleted_at IS NULL
		ORDER BY priority DESC, created_at ASC`)
	if err != nil {
		return nil, appErrs.Internal("list workflow rules failed")
	}
	defer rows.Close()

	rules := make([]Rule, 0)
	for rows.Next() {
		var r Rule
		var branchID sql.NullString
		if err := rows.Scan(&r.ID, &r.Name, &r.TriggerType, &r.TriggerValue, &r.ActionType,
			&r.ActionPayload, &branchID, &r.IsActive, &r.Priority, &r.CreatedAt); err != nil {
			return nil, appErrs.Internal("scan rule failed")
		}
		if branchID.Valid {
			r.BranchID = &branchID.String
		}
		rules = append(rules, r)
	}
	return &ListRulesResponse{Rules: rules}, rows.Err()
}

//encore:api auth method=POST path=/api/v1/workflows tag:owner
func CreateRule(ctx context.Context, req *CreateRuleRequest) (*CreateRuleResponse, error) {
	u, err := user(ctx)
	if err != nil {
		return nil, err
	}
	if err := entitlement.Require(ctx, u.TenantSchema, entitlement.FeatureWorkflow); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.TriggerValue) == "" {
		return nil, appErrs.BadRequest("name and triggerValue required")
	}
	trigger := req.TriggerType
	if trigger == "" {
		trigger = "message_contains"
	}
	action := req.ActionType
	if action == "" {
		action = "send_reply"
	}
	payload := req.ActionPayload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}

	ts, err := openTenantScope(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal("database connection failed")
	}

	var r Rule
	var branchID sql.NullString
	err = ts.QueryRowContext(ctx, `
		INSERT INTO workflow_rule (name, trigger_type, trigger_value, action_type, action_payload, branch_id, priority)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, name, trigger_type, trigger_value, action_type, action_payload, branch_id, is_active, priority, created_at`,
		req.Name, trigger, req.TriggerValue, action, payload, req.BranchID, req.Priority,
	).Scan(&r.ID, &r.Name, &r.TriggerType, &r.TriggerValue, &r.ActionType, &r.ActionPayload,
		&branchID, &r.IsActive, &r.Priority, &r.CreatedAt)
	if err != nil {
		return nil, appErrs.Internal("create workflow rule failed")
	}
	if branchID.Valid {
		r.BranchID = &branchID.String
	}
	return &CreateRuleResponse{Rule: r}, nil
}

//encore:api auth method=PATCH path=/api/v1/workflows/:id tag:owner
func UpdateRule(ctx context.Context, id string, req *UpdateRuleRequest) (*UpdateRuleResponse, error) {
	u, err := user(ctx)
	if err != nil {
		return nil, err
	}
	if err := entitlement.Require(ctx, u.TenantSchema, entitlement.FeatureWorkflow); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.TriggerValue) == "" {
		return nil, appErrs.BadRequest("name and triggerValue required")
	}
	trigger := req.TriggerType
	if trigger == "" {
		trigger = "message_contains"
	}
	action := req.ActionType
	if action == "" {
		action = "send_reply"
	}
	payload := req.ActionPayload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	ts, err := openTenantScope(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal("database connection failed")
	}

	var r Rule
	var branchID sql.NullString
	err = ts.QueryRowContext(ctx, `
		UPDATE workflow_rule
		SET name = $2, trigger_type = $3, trigger_value = $4,
		    action_type = $5, action_payload = $6, priority = $7,
		    is_active = $8, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, name, trigger_type, trigger_value, action_type, action_payload,
		          branch_id, is_active, priority, created_at`,
		id, req.Name, trigger, req.TriggerValue, action, payload, req.Priority, isActive,
	).Scan(&r.ID, &r.Name, &r.TriggerType, &r.TriggerValue, &r.ActionType, &r.ActionPayload,
		&branchID, &r.IsActive, &r.Priority, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("workflow rule not found")
	}
	if err != nil {
		return nil, appErrs.Internal("update workflow rule failed")
	}
	if branchID.Valid {
		r.BranchID = &branchID.String
	}
	return &UpdateRuleResponse{Rule: r}, nil
}

//encore:api auth method=DELETE path=/api/v1/workflows/:id tag:owner
func DeleteRule(ctx context.Context, id string) error {
	u, err := user(ctx)
	if err != nil {
		return err
	}
	if err := entitlement.Require(ctx, u.TenantSchema, entitlement.FeatureWorkflow); err != nil {
		return err
	}
	ts, err := openTenantScope(ctx, u.TenantSchema)
	if err != nil {
		return appErrs.Internal("database connection failed")
	}
	res, err := ts.ExecContext(ctx, `
		UPDATE workflow_rule SET deleted_at = NOW(), updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return appErrs.Internal("delete rule failed")
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return appErrs.NotFound("workflow rule not found")
	}
	return nil
}

// TryRun evaluates active rules for an inbound message. Returns true if a rule handled the message.
func TryRun(ctx context.Context, schema, conversationID, messageBody string) (bool, error) {
	if err := entitlement.Require(ctx, schema, entitlement.FeatureWorkflow); err != nil {
		return false, nil
	}
	if ok, _, limit := usage.CheckQuota(ctx, schema, "workflow_exec"); !ok && limit > 0 {
		return false, nil
	}

	ts, err := openTenantScope(ctx, schema)
	if err != nil {
		return false, err
	}

	bodyLower := strings.ToLower(messageBody)
	rows, err := ts.QueryContext(ctx, `
		SELECT id, trigger_type, trigger_value, action_type, action_payload
		FROM workflow_rule
		WHERE deleted_at IS NULL AND is_active = true
		ORDER BY priority DESC, created_at ASC`)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, triggerType, triggerValue, actionType string
		var payload json.RawMessage
		if err := rows.Scan(&id, &triggerType, &triggerValue, &actionType, &payload); err != nil {
			continue
		}
		matched := false
		switch triggerType {
		case "message_contains":
			matched = strings.Contains(bodyLower, strings.ToLower(triggerValue))
		default:
			matched = strings.Contains(bodyLower, strings.ToLower(triggerValue))
		}
		if !matched {
			continue
		}

		if err := runAction(ctx, ts, schema, conversationID, actionType, payload); err != nil {
			rlog.Warn("workflow action failed", "ruleId", id, "err", err)
			continue
		}
		_ = usage.RecordEvent(ctx, schema, "workflow_exec", 1, nil)
		return true, nil
	}
	return false, rows.Err()
}

func runAction(ctx context.Context, ts appdb.TenantScope, schema, conversationID, actionType string, payload json.RawMessage) error {
	var p struct {
		ReplyText string `json:"replyText"`
	}
	_ = json.Unmarshal(payload, &p)

	switch actionType {
	case "send_reply":
		text := strings.TrimSpace(p.ReplyText)
		if text == "" {
			text = "Terima kasih, tim kami akan segera membantu Anda."
		}
		_, err := ts.ExecContext(ctx, `
			INSERT INTO message (conversation_id, direction, type, body, status, metadata)
			VALUES ($1, 'outbound', 'text', $2, 'sent', '{"reason":"workflow"}'::jsonb)`,
			conversationID, text)
		if err != nil {
			return err
		}
		_, err = ts.ExecContext(ctx, `
			UPDATE conversation SET last_message_at = NOW(), last_message_preview = $1, ai_handled = true WHERE id = $2`,
			text, conversationID)
		return err
	case "handoff":
		_, err := ts.ExecContext(ctx, `
			UPDATE conversation SET ai_handled = false, handoff_reason = 'workflow', status = 'open' WHERE id = $1`,
			conversationID)
		return err
	default:
		return fmt.Errorf("unknown action: %s", actionType)
	}
}

func user(ctx context.Context) (*types.AuthUser, error) {
	u, ok := auth.Data().(*types.AuthUser)
	if !ok || u == nil {
		return nil, appErrs.Unauthenticated("not authenticated")
	}
	if !u.HasEffectiveTenantContext() {
		return nil, appErrs.Forbidden("tenant context required")
	}
	return u, nil
}
