package order

// shouldResyncCompletedOrderIncome reports whether Update should replace the
// finance income row for an order that stays (or is already) completed without
// a status transition in the same request.
func shouldResyncCompletedOrderIncome(
	newStatus, orderStatus string,
	updatedSubtotal, updatedShippingCost *float64,
	walletUpdated bool,
) bool {
	if newStatus != "" {
		return false
	}
	if orderStatus != "completed" {
		return false
	}
	return updatedSubtotal != nil || updatedShippingCost != nil || walletUpdated
}
