package apitest

import (
	"testing"

	"encore.app/wabantu/finance"
)

func TestFinanceSmoke_ListWallets(t *testing.T) {
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)

	resp, err := finance.ListWallets(t.Context())
	if err != nil {
		t.Fatalf("GET /api/v1/finance/wallets: %v", err)
	}
	AssertJSONFields(t, resp, "wallets")
	AssertJSONArrayField(t, resp, "wallets")
}

func TestFinanceSmoke_Dashboard(t *testing.T) {
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)

	resp, err := finance.GetDashboard(t.Context(), &finance.DashboardParams{})
	if err != nil {
		t.Fatalf("GET /api/v1/finance/dashboard: %v", err)
	}
	AssertJSONFields(t, resp,
		"period", "totalIncome", "totalExpense", "netBalance", "totalWallets", "pendingCount", "wallets")
	AssertJSONArrayField(t, resp, "wallets")
}
