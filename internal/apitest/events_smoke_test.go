package apitest

import (
	"fmt"
	"strings"
	"testing"

	"encore.app/wabantu/events"
	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/shared/pii"
	"encore.app/wabantu/tenant"
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

// Regression (fix/tenant-pii-after-events): a freshly registered tenant must be able to
// create an event person (was 500: column "full_name_enc" does not exist), and the row
// must use the encrypted path: full_name holds only the PII placeholder, never plaintext.
func TestEventsSmoke_NewTenantCreatePersonEncrypted(t *testing.T) {
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)
	ctx := t.Context()

	ev, err := events.CreateEvent(ctx, &events.UpsertEventParams{
		EventName: "PII Order Event",
		EventSlug: "pii-order-event",
		StartDate: "2026-12-01",
		EndDate:   "2026-12-01",
		StartTime: "09:00",
		EndTime:   "17:00",
		Status:    "PUBLISHED",
	})
	if err != nil {
		t.Fatalf("POST /api/v1/events: %v", err)
	}

	sch := appdb.SchemaSQL{Schema: fx.SchemaName}
	var roleID string
	if err := tenant.DataDB.Stdlib().QueryRowContext(ctx, fmt.Sprintf(
		`SELECT id::text FROM %s ORDER BY id LIMIT 1`, sch.T("evt_volunteer_role")),
	).Scan(&roleID); err != nil {
		t.Fatalf("seeded volunteer role: %v", err)
	}

	const plainName = "Relawan Sintetis PII"
	person, err := events.CreateEventPerson(ctx, ev.ID, &events.UpsertPersonParams{
		FullName:        plainName,
		Role:            "relawan",
		VolunteerRoleID: &roleID,
	})
	if err != nil {
		t.Fatalf("POST /api/v1/events/detail/:eventId/people (want 201): %v", err)
	}
	if person.FullName != plainName {
		t.Fatalf("response fullName = %q, want decrypted %q", person.FullName, plainName)
	}

	var stored, enc string
	if err := tenant.DataDB.Stdlib().QueryRowContext(ctx, fmt.Sprintf(
		`SELECT full_name, COALESCE(full_name_enc,'') FROM %s WHERE id = $1::uuid`,
		sch.T("evt_event_person")), person.ID,
	).Scan(&stored, &enc); err != nil {
		t.Fatalf("read evt_event_person: %v", err)
	}
	if stored != pii.Placeholder {
		t.Fatalf("full_name = %q, want PII placeholder %q (no plaintext)", stored, pii.Placeholder)
	}
	if enc == "" || strings.Contains(enc, plainName) {
		t.Fatalf("full_name_enc must be non-empty ciphertext without plaintext")
	}
}
