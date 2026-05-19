package billing

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"

	"encore.app/wabantu/shared/types"
)

var db = sqldb.Named("tenant")

// ---------- plan catalog ----------

type PlanLimits struct {
	Channels         int `json:"channels"`
	Seats            int `json:"seats"`
	AIConversations  int `json:"aiConversations"`
	AITokens         int `json:"aiTokens"`
	BroadcastContacts int `json:"broadcastContacts"`
	StorageMB        int `json:"storageMb"`
	WorkflowExecs    int `json:"workflowExecs"`
}

type Plan struct {
	Code      string     `json:"code"`
	Name      string     `json:"name"`
	AmountIDR int        `json:"amountIdr"`
	Limits    PlanLimits `json:"limits"`
}

var PlanCatalog = map[string]Plan{
	"starter": {
		Code: "starter", Name: "Starter", AmountIDR: 299_000,
		Limits: PlanLimits{Channels: 1, Seats: 1, AIConversations: 1_500, AITokens: 2_000_000, BroadcastContacts: 0, StorageMB: 256, WorkflowExecs: 50},
	},
	"business": {
		Code: "business", Name: "Business", AmountIDR: 799_000,
		Limits: PlanLimits{Channels: 2, Seats: 3, AIConversations: 6_000, AITokens: 8_000_000, BroadcastContacts: 500, StorageMB: 2_048, WorkflowExecs: 500},
	},
	"basic": { // legacy alias → business
		Code: "basic", Name: "Business", AmountIDR: 799_000,
		Limits: PlanLimits{Channels: 2, Seats: 3, AIConversations: 6_000, AITokens: 8_000_000, BroadcastContacts: 500, StorageMB: 2_048, WorkflowExecs: 500},
	},
	"pro": {
		Code: "pro", Name: "Pro", AmountIDR: 1_999_000,
		Limits: PlanLimits{Channels: 10, Seats: 10, AIConversations: 20_000, AITokens: 30_000_000, BroadcastContacts: 10_000, StorageMB: 10_240, WorkflowExecs: 5_000},
	},
}

func GetPlanLimits(planCode string) PlanLimits {
	p, ok := PlanCatalog[planCode]
	if !ok {
		return PlanCatalog["starter"].Limits
	}
	return p.Limits
}

// ---------- types ----------

type Subscription struct {
	ID          string     `json:"id"`
	PlanCode    string     `json:"planCode"`
	PlanName    string     `json:"planName"`
	IsTrial     bool       `json:"isTrial"`
	TrialEndsAt *time.Time `json:"trialEndsAt"`
	Status      string     `json:"status"`
	Provider    *string    `json:"provider"`
	ProviderRef *string    `json:"providerRef"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type Invoice struct {
	ID        string     `json:"id"`
	InvoiceNo string     `json:"invoiceNo"`
	PlanCode  string     `json:"planCode"`
	PlanName  string     `json:"planName"`
	AmountIDR int        `json:"amountIdr"`
	Status    string     `json:"status"`
	IssuedAt  time.Time  `json:"issuedAt"`
	PaidAt    *time.Time `json:"paidAt"`
	CreatedAt time.Time  `json:"createdAt"`
}

// ---------- helpers ----------

func tenantDB(ctx context.Context, schema string) (*sql.DB, error) {
	stdlib := db.Stdlib()
	_, err := stdlib.ExecContext(ctx, fmt.Sprintf(`SET search_path TO %q`, schema))
	if err != nil {
		return nil, err
	}
	return stdlib, nil
}

func ensureSubscription(ctx context.Context, d *sql.DB) (*Subscription, error) {
	var s Subscription
	err := d.QueryRowContext(ctx,
		`SELECT id, plan_code, plan_name, is_trial, trial_ends_at, status, provider, provider_ref, created_at, updated_at
		 FROM subscription ORDER BY created_at DESC LIMIT 1`,
	).Scan(&s.ID, &s.PlanCode, &s.PlanName, &s.IsTrial, &s.TrialEndsAt, &s.Status, &s.Provider, &s.ProviderRef, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		trial := time.Now().Add(7 * 24 * time.Hour)
		err = d.QueryRowContext(ctx,
			`INSERT INTO subscription (plan_code, plan_name, is_trial, trial_ends_at, status)
			 VALUES ('starter','Starter',true,$1,'active')
			 RETURNING id, plan_code, plan_name, is_trial, trial_ends_at, status, provider, provider_ref, created_at, updated_at`,
			trial,
		).Scan(&s.ID, &s.PlanCode, &s.PlanName, &s.IsTrial, &s.TrialEndsAt, &s.Status, &s.Provider, &s.ProviderRef, &s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return nil, err
		}
		return &s, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ---------- API ----------

type OverviewResponse struct {
	Subscription *Subscription `json:"subscription"`
	Plans        []Plan        `json:"plans"`
	Invoices     []Invoice     `json:"invoices"`
}

//encore:api auth method=GET path=/api/v1/billing/overview
func Overview(ctx context.Context) (*OverviewResponse, error) {
	uid, err := authUser(ctx)
	if err != nil {
		return nil, err
	}
	d, err := tenantDB(ctx, uid.TenantSchema)
	if err != nil {
		return nil, err
	}
	sub, err := ensureSubscription(ctx, d)
	if err != nil {
		return nil, err
	}
	rows, err := d.QueryContext(ctx,
		`SELECT id, invoice_no, plan_code, plan_name, amount_idr, status, issued_at, paid_at, created_at
		 FROM invoice ORDER BY issued_at DESC LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var invoices []Invoice
	for rows.Next() {
		var inv Invoice
		if err := rows.Scan(&inv.ID, &inv.InvoiceNo, &inv.PlanCode, &inv.PlanName, &inv.AmountIDR, &inv.Status, &inv.IssuedAt, &inv.PaidAt, &inv.CreatedAt); err != nil {
			rlog.Error("scan invoice", "err", err)
			continue
		}
		invoices = append(invoices, inv)
	}
	plans := make([]Plan, 0, len(PlanCatalog))
	for _, p := range PlanCatalog {
		plans = append(plans, p)
	}
	return &OverviewResponse{Subscription: sub, Plans: plans, Invoices: invoices}, nil
}

type SelectPlanRequest struct {
	PlanCode string  `json:"planCode"`
	Provider *string `json:"provider,omitempty"`
}
type SelectPlanResponse struct {
	Subscription *Subscription `json:"subscription"`
}

//encore:api auth method=POST path=/api/v1/billing/select-plan
func SelectPlan(ctx context.Context, req *SelectPlanRequest) (*SelectPlanResponse, error) {
	uid, err := authUser(ctx)
	if err != nil {
		return nil, err
	}
	if !uid.CanPerformOwnerActions() {
		return nil, &errs.Error{Code: errs.PermissionDenied, Message: "owner only"}
	}
	plan, ok := PlanCatalog[req.PlanCode]
	if !ok {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "plan tidak valid"}
	}
	d, err := tenantDB(ctx, uid.TenantSchema)
	if err != nil {
		return nil, err
	}
	sub, err := ensureSubscription(ctx, d)
	if err != nil {
		return nil, err
	}
	provRef := ""
	if req.Provider != nil && *req.Provider != "" {
		provRef = fmt.Sprintf("%s_%s", *req.Provider, randStr(8))
	}
	var prov *string
	var pr *string
	if req.Provider != nil && *req.Provider != "" {
		prov = req.Provider
		pr = &provRef
	}
	err = d.QueryRowContext(ctx,
		`UPDATE subscription SET plan_code=$1, plan_name=$2, is_trial=false, trial_ends_at=NULL, provider=$3, provider_ref=$4, updated_at=now()
		 WHERE id=$5
		 RETURNING id, plan_code, plan_name, is_trial, trial_ends_at, status, provider, provider_ref, created_at, updated_at`,
		req.PlanCode, plan.Name, prov, pr, sub.ID,
	).Scan(&sub.ID, &sub.PlanCode, &sub.PlanName, &sub.IsTrial, &sub.TrialEndsAt, &sub.Status, &sub.Provider, &sub.ProviderRef, &sub.CreatedAt, &sub.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if plan.AmountIDR > 0 {
		invNo := fmt.Sprintf("INV-%s-%s", time.Now().Format("20060102"), randStr(6))
		_, err = d.ExecContext(ctx,
			`INSERT INTO invoice (invoice_no, plan_code, plan_name, amount_idr, status, issued_at)
			 VALUES ($1,$2,$3,$4,'issued',now())`,
			invNo, req.PlanCode, plan.Name, plan.AmountIDR)
		if err != nil {
			rlog.Error("create invoice", "err", err)
		}
	}
	return &SelectPlanResponse{Subscription: sub}, nil
}

type InvoicesResponse struct {
	Invoices []Invoice `json:"invoices"`
}

//encore:api auth method=GET path=/api/v1/billing/invoices
func ListInvoices(ctx context.Context) (*InvoicesResponse, error) {
	uid, err := authUser(ctx)
	if err != nil {
		return nil, err
	}
	d, err := tenantDB(ctx, uid.TenantSchema)
	if err != nil {
		return nil, err
	}
	rows, err := d.QueryContext(ctx,
		`SELECT id, invoice_no, plan_code, plan_name, amount_idr, status, issued_at, paid_at, created_at
		 FROM invoice ORDER BY issued_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var invoices []Invoice
	for rows.Next() {
		var inv Invoice
		if err := rows.Scan(&inv.ID, &inv.InvoiceNo, &inv.PlanCode, &inv.PlanName, &inv.AmountIDR, &inv.Status, &inv.IssuedAt, &inv.PaidAt, &inv.CreatedAt); err != nil {
			continue
		}
		invoices = append(invoices, inv)
	}
	return &InvoicesResponse{Invoices: invoices}, nil
}

// ---------- internal ----------

func authUser(ctx context.Context) (*types.AuthUser, error) {
	u, ok := auth.Data().(*types.AuthUser)
	if !ok || u == nil {
		return nil, &errs.Error{Code: errs.Unauthenticated, Message: "not authenticated"}
	}
	return u, nil
}

func randStr(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
