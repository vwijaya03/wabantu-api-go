package orderapprove

import "context"

// StockStore is the per-order stock marker (order.stock_applied_at) plus the deduction.
type StockStore interface {
	// ClaimStock atomically sets stock_applied_at where it IS NULL and returns the claimed
	// value as a token; claimed=false when stock was already applied (or is being applied).
	ClaimStock(ctx context.Context) (token string, claimed bool, err error)
	// ApplyStock deducts stock (inventory.SyncOrderStock; all-or-nothing tx).
	ApplyStock(ctx context.Context) error
	// ReleaseStock clears the marker only if it still holds token (compare-and-clear).
	ReleaseStock(ctx context.Context, token string) error
}

// ApplyStockOnce runs the deduction at most once per order across retries and concurrent
// callers: only the caller that wins the claim deducts. If the deduction fails the claim is
// released (compare-and-clear on the token) so a retry can apply it. Releasing is safe
// because the deduction is all-or-nothing and its own tx re-stamps the marker on success,
// so a failure after the stock commit (e.g. COGS) no longer matches the token and the
// marker stays set.
func ApplyStockOnce(ctx context.Context, s StockStore) (applied bool, err error) {
	token, claimed, err := s.ClaimStock(ctx)
	if err != nil || !claimed {
		return false, err
	}
	if err := s.ApplyStock(ctx); err != nil {
		if rerr := s.ReleaseStock(ctx, token); rerr != nil {
			return false, rerr
		}
		return false, err
	}
	return true, nil
}
