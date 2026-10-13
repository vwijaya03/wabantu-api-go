// Package orderapprove holds the owner "approve" flow for an order (verify payment proof +
// complete + finance income) as runtime-free logic: the decision table and the
// transactional orchestration over a small Store interface, so it is unit-testable
// without Encore. order.Approve wires it to Postgres.
package orderapprove

import (
	"context"
	"errors"
	"strings"
)

// State is the locked (SELECT … FOR UPDATE) order row.
type State struct {
	Status         string
	PaymentStatus  string
	ProofMessageID string // payment_proof_message_id
	HasProofMeta   bool   // payment_proof_meta has OCR data
	ProofBlocked   bool
}

type Decision int

const (
	Proceed     Decision = iota // verify (if needed) + complete + income
	AlreadyDone                 // completed already: 200, no writes, no duplicate income
	Reject                      // see Error
)

// Error is a client error with an HTTP-ish kind.
type Error struct {
	Kind    string // "conflict" (409) | "unprocessable" (422)
	Message string
}

func (e *Error) Error() string { return e.Message }

var (
	ErrNoProof   = &Error{Kind: "unprocessable", Message: "pesanan belum memiliki bukti pembayaran"}
	ErrCancelled = &Error{Kind: "conflict", Message: "pesanan sudah dibatalkan"}
	ErrBlocked   = &Error{Kind: "conflict", Message: "bukti pembayaran diblokir; buka blokir dulu"}
	ErrDraft     = &Error{Kind: "conflict", Message: "pesanan masih draft; konfirmasi pesanan dulu"}
)

// Decide applies the approve rules to a locked order row.
func Decide(s State) (Decision, *Error) {
	status := strings.ToLower(strings.TrimSpace(s.Status))
	pay := strings.ToLower(strings.TrimSpace(s.PaymentStatus))
	switch status {
	case "completed":
		return AlreadyDone, nil
	case "cancelled":
		return Reject, ErrCancelled
	}
	if s.ProofBlocked {
		return Reject, ErrBlocked
	}
	hasProof := pay == "verified" || pay == "proof_submitted" ||
		strings.TrimSpace(s.ProofMessageID) != "" || s.HasProofMeta
	if !hasProof {
		return Reject, ErrNoProof
	}
	if status == "draft" && pay != "verified" && pay != "proof_submitted" {
		return Reject, ErrDraft
	}
	return Proceed, nil
}

// Store is one database transaction (implemented over *sql.Tx in package order).
type Store interface {
	LockOrder(ctx context.Context) (State, bool, error) // found=false → not found
	// CompleteVerified sets payment_status=verified (keeping an earlier verifier) and status=completed.
	CompleteVerified(ctx context.Context) error
	// RecordIncome inserts the finance income row (idempotent per order).
	RecordIncome(ctx context.Context) error
	// NotifyBuyer stores the buyer-facing system message (optional, inside the tx).
	NotifyBuyer(ctx context.Context) error
	Commit() error
	Rollback() error
}

var ErrNotFound = errors.New("order not found")

// Result reports what Run did.
type Result struct {
	Decision Decision
	Prev     State
}

// Run executes the approve flow in ONE transaction: lock → decide → complete+verify →
// income → buyer message → commit. Any error rolls back everything. An already-completed
// order is a no-op (rolled back read-only tx) so retries never duplicate income.
func Run(ctx context.Context, st Store) (res Result, err error) {
	committed := false
	defer func() {
		if !committed {
			_ = st.Rollback()
		}
	}()
	s, found, err := st.LockOrder(ctx)
	if err != nil {
		return res, err
	}
	if !found {
		return res, ErrNotFound
	}
	res.Prev = s
	d, derr := Decide(s)
	res.Decision = d
	switch d {
	case AlreadyDone:
		return res, nil
	case Reject:
		return res, derr
	}
	if err := st.CompleteVerified(ctx); err != nil {
		return res, err
	}
	if err := st.RecordIncome(ctx); err != nil {
		return res, err
	}
	if err := st.NotifyBuyer(ctx); err != nil {
		return res, err
	}
	if err := st.Commit(); err != nil {
		return res, err
	}
	committed = true
	return res, nil
}
