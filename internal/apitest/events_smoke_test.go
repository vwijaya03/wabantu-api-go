package apitest

import (
	"testing"

	"encore.app/wabantu/events"
)

func TestEventsSmoke_ListEvents(t *testing.T) {
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)

	resp, err := events.ListEvents(t.Context(), &events.ListEventsParams{})
	if err != nil {
		t.Fatalf("GET /api/v1/events: %v", err)
	}
	AssertJSONFields(t, resp, "items", "total")
	AssertJSONArrayField(t, resp, "items")
}

func TestEventsSmoke_PublicRegistration(t *testing.T) {
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)

	created, err := events.CreateEvent(t.Context(), &events.UpsertEventParams{
		EventName: "Smoke Event",
		EventSlug: "smoke-event",
		StartDate: "2026-12-01",
		EndDate:   "2026-12-01",
		StartTime: "09:00",
		EndTime:   "17:00",
		Status:    "PUBLISHED",
	})
	if err != nil {
		t.Fatalf("POST /api/v1/events: %v", err)
	}
	if created.EventSlug == "" {
		t.Fatal("created event missing slug")
	}

	info, err := events.GetPublicRegistration(t.Context(), fx.TenantSlug, created.EventSlug)
	if err != nil {
		t.Fatalf("GET /api/v1/public/events/:tenantSlug/register/:eventSlug: %v", err)
	}
	AssertJSONFields(t, info, "eventName", "startDate", "endDate", "status", "therapies")
	AssertJSONArrayField(t, info, "therapies")
}
