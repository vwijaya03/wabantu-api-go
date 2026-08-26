package apitest

import (
	"testing"

	"encore.app/wabantu/analytics"
)

func TestAnalyticsSmoke_Overview(t *testing.T) {
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)

	resp, err := analytics.Overview(t.Context(), &analytics.OverviewRequest{Days: 30})
	if err != nil {
		t.Fatalf("GET /api/v1/analytics/overview: %v", err)
	}
	AssertJSONFields(t, resp, "windowDays", "totals", "today", "reportingTimezone", "overview", "kpis", "topQuestions")
	AssertJSONArrayField(t, resp, "topQuestions")
}
