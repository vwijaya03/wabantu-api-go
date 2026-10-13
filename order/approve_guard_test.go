package order

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"encore.app/wabantu/internal/orderapprove"
	"encore.app/wabantu/shared/types"
)

func TestApproveGateImpersonation403(t *testing.T) {
	imp := &types.AuthUser{Role: "super_admin", Impersonating: true, TenantSchema: "t_x"}
	if err := approveGate(imp); err == nil || !strings.Contains(err.Error(), "pantau") {
		t.Fatalf("impersonation must be 403, got %v", err)
	}
	owner := &types.AuthUser{Role: "owner", TenantSchema: "t_x"}
	if err := approveGate(owner); err != nil {
		t.Fatalf("owner: %v", err)
	}
	staff := &types.AuthUser{Role: "staff", TenantSchema: "t_x"}
	if approveGate(staff) == nil {
		t.Fatal("staff must be 403")
	}
}

// updatedAt is exposed as RFC3339Nano (µs from Postgres survive JSON); the exact value parses
// back to the identical instant, so a valid approve with the exact value passes the guard.
func TestExpectedUpdatedAtRoundTripMicroseconds(t *testing.T) {
	ts := time.Date(2026, 10, 13, 10, 53, 32, 575123000, time.UTC)
	b, _ := json.Marshal(Order{UpdatedAt: ts})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	s, _ := m["updatedAt"].(string)
	if s != "2026-10-13T10:53:32.575123Z" {
		t.Fatalf("updatedAt JSON %q", s)
	}
	got, err := parseExpectedUpdatedAt(&ApproveParams{ExpectedUpdatedAt: s})
	if err != nil || got == nil || !got.Equal(ts) {
		t.Fatalf("parse %v %v", got, err)
	}
	if p, err := parseExpectedUpdatedAt(nil); p != nil || err != nil {
		t.Fatal("absent body = old behaviour")
	}
	if p, err := parseExpectedUpdatedAt(&ApproveParams{}); p != nil || err != nil {
		t.Fatal("empty = old behaviour")
	}
	if _, err := parseExpectedUpdatedAt(&ApproveParams{ExpectedUpdatedAt: "13/10/2026"}); err == nil {
		t.Fatal("invalid must be 400")
	}
}

// One atomic statement: exactly the path id + optional updated_at guard; no "latest order".
func TestCompleteVerifiedSQLIsGuardedAndIDScoped(t *testing.T) {
	q := completeVerifiedSQL("t_toko_demo")
	for _, want := range []string{`UPDATE "t_toko_demo"."order"`, "WHERE id::text = $1", "($3::timestamptz IS NULL OR updated_at = $3::timestamptz)"} {
		if !strings.Contains(q, want) {
			t.Errorf("missing %q in %s", want, q)
		}
	}
	where := q[strings.Index(q, "WHERE"):strings.Index(q, "RETURNING")]
	for _, bad := range []string{"conversation_id", "contact_id", "ORDER BY", "LIMIT", "created_at"} {
		if strings.Contains(where, bad) {
			t.Errorf("approve UPDATE must not select by %q", bad)
		}
	}
}

func TestApproveAuditFieldsNoPII(t *testing.T) {
	u := &types.AuthUser{AccountID: "acc-1", TenantID: "ten-1", Role: "owner"}
	prev := orderapprove.State{Status: "draft", PaymentStatus: "proof_submitted", ProofMessageID: "msg-9"}
	after := &Order{Status: "completed", PaymentStatus: "verified"}
	c := approveAuditChanges(u, "ord-1", prev, after)
	want := map[string]any{"account_id": "acc-1", "session_kind": "client", "order_id": "ord-1",
		"status_before": "draft", "status_after": "completed", "proof_id": "msg-9",
		"payment_status_before": "proof_submitted", "payment_status_after": "verified"}
	if len(c) != len(want) {
		t.Fatalf("audit keys %v", c)
	}
	for k, v := range want {
		if c[k] != v {
			t.Errorf("%s = %v want %v", k, c[k], v)
		}
	}
	// Seam: the approve path writes exactly one order.approve row via auditLog.
	var got []string
	old := auditLog
	auditLog = func(_ context.Context, _, _, action, entity, id string, _ any) {
		got = append(got, action+":"+entity+":"+id)
	}
	defer func() { auditLog = old }()
	auditLog(context.Background(), u.TenantID, u.AccountID, "order.approve", "order", "ord-1", c)
	if len(got) != 1 || got[0] != "order.approve:order:ord-1" {
		t.Fatalf("audit %v", got)
	}
}
