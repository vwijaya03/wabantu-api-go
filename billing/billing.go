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

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/shared/types"
)

var db = sqldb.Named("tenant")

// ---------- plan catalog ----------

type PlanLimits struct {
	Channels          int `json:"channels"`
	Seats             int `json:"seats"`
	AIConversations   int `json:"aiConversations"`
	AITokens          int `json:"aiTokens"`
	BroadcastContacts int `json:"broadcastContacts"`
	StorageMB         int `json:"storageMb"`
	WorkflowExecs     int `json:"workflowExecs"`
}

type Plan struct {
	Code      string     `json:"code"`
	Name      string     `json:"name"`
	AmountIDR int        `json:"amountIdr"`
	Limits    PlanLimits `json:"limits"`
}

type TopUpOption struct {
	Code            string `json:"code"`
	Name            string `json:"name"`
	AmountIDR       int    `json:"amountIdr"`
	AITokens        int    `json:"aiTokens"`
	AIConversations int    `json:"aiConversations"`
	ValidForPeriod  string `json:"validForPeriod"`
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
	"basic": { // legacy alias → business (API only, not shown in catalog)
		Code: "basic", Name: "Business", AmountIDR: 799_000,
		Limits: PlanLimits{Channels: 2, Seats: 3, AIConversations: 6_000, AITokens: 8_000_000, BroadcastContacts: 500, StorageMB: 2_048, WorkflowExecs: 500},
	},
	"pro": {
		Code: "pro", Name: "Pro", AmountIDR: 1_999_000,
		Limits: PlanLimits{Channels: 10, Seats: 10, AIConversations: 20_000, AITokens: 30_000_000, BroadcastContacts: 10_000, StorageMB: 10_240, WorkflowExecs: 5_000},
	},
}

// Trial limits (enforced via usage.TenantPlan when subscription.is_trial=true).
var trialLimits = PlanLimits{
	Channels: 1, Seats: 1, AIConversations: 60, AITokens: 100_000,
	BroadcastContacts: 20, StorageMB: 50, WorkflowExecs: 8,
}

// sellablePlanOrder is the UI/API catalog (no legacy duplicate "basic").
var sellablePlanOrder = []string{"starter", "business", "pro"}

var aiTopUpOptions = []TopUpOption{
	{
		Code: "topup_ai_20000", Name: "AI Top-up 20rb", AmountIDR: 20_000,
		// UNIT_ECONOMICS_AND_PRICING.md: Rp75k ≈ 500k token.
		// 20k prorata = 133k token; conversations rounded down at ~2.250 token/chat.
		AITokens: 133_000, AIConversations: 59,
	},
	{
		Code: "topup_ai_30000", Name: "AI Top-up 30rb", AmountIDR: 30_000,
		AITokens: 200_000, AIConversations: 88,
	},
}

func normalizePlanCode(code string) string {
	if code == "basic" {
		return "business"
	}
	return code
}

func resolvePlan(code string) (Plan, bool) {
	code = normalizePlanCode(code)
	if code == "trial" {
		return Plan{Code: "trial", Name: "Trial", AmountIDR: 0, Limits: trialLimits}, true
	}
	p, ok := PlanCatalog[code]
	if !ok && code == "basic" {
		p, ok = PlanCatalog["basic"]
	}
	return p, ok
}

func listSellablePlans() []Plan {
	out := make([]Plan, 0, len(sellablePlanOrder))
	for _, code := range sellablePlanOrder {
		if p, ok := PlanCatalog[code]; ok {
			out = append(out, p)
		}
	}
	return out
}

func listTopUpOptions(period string) []TopUpOption {
	out := make([]TopUpOption, 0, len(aiTopUpOptions))
	for _, opt := range aiTopUpOptions {
		opt.ValidForPeriod = period
		out = append(out, opt)
	}
	return out
}

func resolveTopUp(code string) (TopUpOption, bool) {
	for _, opt := range aiTopUpOptions {
		if opt.Code == code {
			return opt, true
		}
	}
	return TopUpOption{}, false
}

func GetPlanLimits(planCode string) PlanLimits {
	if planCode == "trial" {
		return trialLimits
	}
	p, ok := resolvePlan(planCode)
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

func tenantConn(ctx context.Context, schema string) (*sql.Conn, error) {
	if schema == "" {
		return nil, &errs.Error{Code: errs.PermissionDenied, Message: "tenant context required"}
	}
	return appdb.TenantConn(ctx, db.Stdlib(), schema)
}

func ensureSubscription(ctx context.Context, conn *sql.Conn) (*Subscription, error) {
	var s Subscription
	err := conn.QueryRowContext(ctx,
		`SELECT id, plan_code, plan_name, is_trial, trial_ends_at, status, provider, provider_ref, created_at, updated_at
		 FROM subscription ORDER BY created_at DESC LIMIT 1`,
	).Scan(&s.ID, &s.PlanCode, &s.PlanName, &s.IsTrial, &s.TrialEndsAt, &s.Status, &s.Provider, &s.ProviderRef, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		trial := time.Now().Add(7 * 24 * time.Hour)
		err = conn.QueryRowContext(ctx,
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
	Subscription    *Subscription `json:"subscription"`
	Plans           []Plan        `json:"plans"`
	TopUpOptions    []TopUpOption `json:"topUpOptions"`
	Invoices        []Invoice     `json:"invoices"`
	PendingCheckout *Invoice      `json:"pendingCheckout,omitempty"`
}

//encore:api auth method=GET path=/api/v1/billing/overview
func Overview(ctx context.Context) (*OverviewResponse, error) {
	uid, err := authUser(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, uid.TenantSchema)
	if err != nil {
		return nil, err
	}
	defer appdb.CloseTenantConn(conn)
	sub, err := ensureSubscription(ctx, conn)
	if err != nil {
		return nil, err
	}
	rows, err := conn.QueryContext(ctx,
		`SELECT id, invoice_no, plan_code, plan_name, amount_idr, status, issued_at, paid_at, created_at
		 FROM invoice
		 WHERE status IN ('paid','issued')
		 ORDER BY COALESCE(paid_at, issued_at) DESC LIMIT 20`)
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
	var pending *Invoice
	var p Invoice
	err = conn.QueryRowContext(ctx,
		`SELECT id, invoice_no, plan_code, plan_name, amount_idr, status, issued_at, paid_at, created_at
		 FROM invoice WHERE status='pending' ORDER BY issued_at DESC LIMIT 1`,
	).Scan(&p.ID, &p.InvoiceNo, &p.PlanCode, &p.PlanName, &p.AmountIDR, &p.Status, &p.IssuedAt, &p.PaidAt, &p.CreatedAt)
	if err == nil {
		pending = &p
	} else if err != sql.ErrNoRows {
		return nil, err
	}
	return &OverviewResponse{
		Subscription:    sub,
		Plans:           listSellablePlans(),
		TopUpOptions:    listTopUpOptions(time.Now().Format("2006-01")),
		Invoices:        invoices,
		PendingCheckout: pending,
	}, nil
}

type SelectPlanRequest struct {
	PlanCode string  `json:"planCode"`
	Provider *string `json:"provider,omitempty"`
}
type SelectPlanResponse struct {
	Subscription   *Subscription `json:"subscription"`
	PendingInvoice *Invoice      `json:"pendingInvoice,omitempty"`
}

type CreateTopUpRequest struct {
	Code string `json:"code"`
}

type CreateTopUpResponse struct {
	TopUp          TopUpOption `json:"topUp"`
	PendingInvoice *Invoice    `json:"pendingInvoice,omitempty"`
}

// SelectPlan starts checkout: creates a pending invoice only. Subscription upgrades after QRIS payment (webhook).
//
//encore:api auth method=POST path=/api/v1/billing/select-plan
func SelectPlan(ctx context.Context, req *SelectPlanRequest) (*SelectPlanResponse, error) {
	uid, err := authUser(ctx)
	if err != nil {
		return nil, err
	}
	if !uid.CanPerformOwnerActions() {
		return nil, &errs.Error{Code: errs.PermissionDenied, Message: "owner only"}
	}
	plan, ok := resolvePlan(req.PlanCode)
	if !ok {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "plan tidak valid"}
	}
	if plan.AmountIDR <= 0 {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "paket gratis tidak perlu checkout"}
	}
	conn, err := tenantConn(ctx, uid.TenantSchema)
	if err != nil {
		return nil, err
	}
	defer appdb.CloseTenantConn(conn)
	sub, err := ensureSubscription(ctx, conn)
	if err != nil {
		return nil, err
	}
	// Replace older unpaid checkouts.
	_, _ = conn.ExecContext(ctx, `UPDATE invoice SET status='void' WHERE status='pending'`)

	invNo := fmt.Sprintf("INV-%s-%s", time.Now().Format("20060102"), randStr(6))
	var inv Invoice
	err = conn.QueryRowContext(ctx,
		`INSERT INTO invoice (invoice_no, plan_code, plan_name, amount_idr, status, issued_at)
		 VALUES ($1,$2,$3,$4,'pending',now())
		 RETURNING id, invoice_no, plan_code, plan_name, amount_idr, status, issued_at, paid_at, created_at`,
		invNo, plan.Code, plan.Name, plan.AmountIDR,
	).Scan(&inv.ID, &inv.InvoiceNo, &inv.PlanCode, &inv.PlanName, &inv.AmountIDR, &inv.Status, &inv.IssuedAt, &inv.PaidAt, &inv.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &SelectPlanResponse{Subscription: sub, PendingInvoice: &inv}, nil
}

// CreateTopUpCheckout creates a pending invoice for one-off AI quota top-up.
//
// Top-ups are strict add-ons for the current calendar month. They do not change
// subscription plan and are activated only after QRIS payment webhook succeeds.
//
//encore:api auth method=POST path=/api/v1/billing/top-up
func CreateTopUpCheckout(ctx context.Context, req *CreateTopUpRequest) (*CreateTopUpResponse, error) {
	uid, err := authUser(ctx)
	if err != nil {
		return nil, err
	}
	if !uid.CanPerformOwnerActions() {
		return nil, &errs.Error{Code: errs.PermissionDenied, Message: "owner only"}
	}
	opt, ok := resolveTopUp(req.Code)
	if !ok {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "top-up tidak valid"}
	}
	opt.ValidForPeriod = time.Now().Format("2006-01")
	conn, err := tenantConn(ctx, uid.TenantSchema)
	if err != nil {
		return nil, err
	}
	defer appdb.CloseTenantConn(conn)
	var hasPending bool
	if err := conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM invoice WHERE status='pending')`).Scan(&hasPending); err != nil {
		return nil, err
	}
	if hasPending {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "selesaikan invoice pending terlebih dahulu"}
	}

	invNo := fmt.Sprintf("TOPUP-%s-%s", time.Now().Format("20060102"), randStr(6))
	var inv Invoice
	err = conn.QueryRowContext(ctx,
		`INSERT INTO invoice (invoice_no, plan_code, plan_name, amount_idr, status, issued_at)
		 VALUES ($1,$2,$3,$4,'pending',now())
		 RETURNING id, invoice_no, plan_code, plan_name, amount_idr, status, issued_at, paid_at, created_at`,
		invNo, opt.Code, opt.Name, opt.AmountIDR,
	).Scan(&inv.ID, &inv.InvoiceNo, &inv.PlanCode, &inv.PlanName, &inv.AmountIDR, &inv.Status, &inv.IssuedAt, &inv.PaidAt, &inv.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &CreateTopUpResponse{TopUp: opt, PendingInvoice: &inv}, nil
}

type ActivatePaidInvoiceParams struct {
	TenantSchema string `json:"tenantSchema"`
	InvoiceID    string `json:"invoiceId"`
}

// ActivatePaidInvoice applies a paid invoice to the active subscription (called from payment webhook).
//
//encore:api private method=POST path=/api/v1/billing/activate-paid-invoice
func ActivatePaidInvoice(ctx context.Context, p *ActivatePaidInvoiceParams) error {
	if p.TenantSchema == "" || p.InvoiceID == "" {
		return &errs.Error{Code: errs.InvalidArgument, Message: "tenantSchema and invoiceId required"}
	}
	conn, err := tenantConn(ctx, p.TenantSchema)
	if err != nil {
		return err
	}
	defer appdb.CloseTenantConn(conn)
	var inv Invoice
	err = conn.QueryRowContext(ctx,
		`SELECT id, invoice_no, plan_code, plan_name, amount_idr, status, issued_at, paid_at, created_at
		 FROM invoice WHERE id=$1`,
		p.InvoiceID,
	).Scan(&inv.ID, &inv.InvoiceNo, &inv.PlanCode, &inv.PlanName, &inv.AmountIDR, &inv.Status, &inv.IssuedAt, &inv.PaidAt, &inv.CreatedAt)
	if err == sql.ErrNoRows {
		return &errs.Error{Code: errs.NotFound, Message: "invoice tidak ditemukan"}
	}
	if err != nil {
		return err
	}
	if inv.Status != "pending" && inv.Status != "paid" {
		return &errs.Error{Code: errs.FailedPrecondition, Message: "invoice tidak bisa diaktifkan"}
	}
	if topUp, ok := resolveTopUp(inv.PlanCode); ok {
		if err := applyPaidTopUp(ctx, conn, inv, topUp); err != nil {
			return err
		}
		_, err = conn.ExecContext(ctx,
			`UPDATE invoice SET status='paid', paid_at=COALESCE(paid_at, now()), issued_at=now()
			 WHERE id=$1 AND status IN ('pending','paid')`,
			inv.ID,
		)
		return err
	}
	plan, ok := resolvePlan(inv.PlanCode)
	if !ok {
		return &errs.Error{Code: errs.Internal, Message: "plan invoice tidak valid"}
	}
	sub, err := ensureSubscription(ctx, conn)
	if err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx,
		`UPDATE subscription SET plan_code=$1, plan_name=$2, is_trial=false, trial_ends_at=NULL,
		 provider='midtrans', updated_at=now()
		 WHERE id=$3`,
		plan.Code, plan.Name, sub.ID,
	)
	if err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx,
		`UPDATE invoice SET status='paid', paid_at=COALESCE(paid_at, now()), issued_at=now()
		 WHERE id=$1 AND status IN ('pending','paid')`,
		inv.ID,
	)
	return err
}

func applyPaidTopUp(ctx context.Context, conn *sql.Conn, inv Invoice, topUp TopUpOption) error {
	period := time.Now().Format("2006-01")
	var exists bool
	if err := conn.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM quota_topup WHERE invoice_id=$1 AND status='paid')`,
		inv.ID,
	).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}

	entries := []struct {
		eventType string
		quantity  int
	}{
		{eventType: "ai_token", quantity: topUp.AITokens},
		{eventType: "ai_conversation", quantity: topUp.AIConversations},
	}
	for _, entry := range entries {
		if entry.quantity <= 0 {
			continue
		}
		if _, err := conn.ExecContext(ctx,
			`INSERT INTO quota_topup
			 (invoice_id, topup_code, event_type, period, quantity, amount_idr, status)
			 VALUES ($1,$2,$3,$4,$5,$6,'paid')`,
			inv.ID, topUp.Code, entry.eventType, period, entry.quantity, topUp.AmountIDR,
		); err != nil {
			return err
		}
	}
	return nil
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
	conn, err := tenantConn(ctx, uid.TenantSchema)
	if err != nil {
		return nil, err
	}
	defer appdb.CloseTenantConn(conn)
	rows, err := conn.QueryContext(ctx,
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
	if !u.HasEffectiveTenantContext() {
		return nil, &errs.Error{Code: errs.PermissionDenied, Message: "tenant context required"}
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
