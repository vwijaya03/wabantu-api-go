package events

import (
	"context"
	"errors"
	"strings"
	"testing"

	encoreerrs "encore.dev/beta/errs"

	appErrs "encore.app/wabantu/shared/errs"
)

func TestIsPublicTransientDBErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"bad connection", errors.New("driver: bad connection"), true},
		{"missing relation", errors.New(`pq: relation "evt_event" does not exist`), true},
		{"other", errors.New("something else"), false},
		{"does not exist alone", errors.New("file does not exist"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isPublicTransientDBErr(tc.err); got != tc.want {
				t.Fatalf("isPublicTransientDBErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestClassifyPublicEventErr_TransientSafeMessage(t *testing.T) {
	ctx := context.Background()
	raw := errors.New(`pq: relation "evt_event" does not exist`)
	got := classifyPublicEventErr(ctx, raw, "demo-tenant", "demo-event")

	var ee *encoreerrs.Error
	if !errors.As(got, &ee) {
		t.Fatalf("expected *errs.Error, got %T %v", got, got)
	}
	if ee.Code != encoreerrs.Unavailable {
		t.Fatalf("code = %v, want Unavailable", ee.Code)
	}
	if ee.Message != msgPublicUnavailable {
		t.Fatalf("message = %q, want %q", ee.Message, msgPublicUnavailable)
	}
	msgLower := strings.ToLower(ee.Message)
	if strings.Contains(msgLower, "evt_") || strings.Contains(msgLower, "relation") {
		t.Fatalf("client message leaked DB details: %q", ee.Message)
	}
	details, ok := ee.Details.(publicEventErrorDetails)
	if !ok || details.ErrorCode != errCodePublicUnavailable {
		t.Fatalf("details = %#v, want errorCode %s", ee.Details, errCodePublicUnavailable)
	}
}

func TestClassifyPublicEventErr_BadConnection(t *testing.T) {
	ctx := context.Background()
	got := classifyPublicEventErr(ctx, appErrs.Internal("driver: bad connection"), "t", "e")
	var ee *encoreerrs.Error
	if !errors.As(got, &ee) {
		t.Fatal("expected *errs.Error")
	}
	if ee.Code != encoreerrs.Unavailable {
		t.Fatalf("code = %v, want Unavailable", ee.Code)
	}
	if strings.Contains(strings.ToLower(ee.Message), "bad connection") {
		t.Fatalf("leaked bad connection: %q", ee.Message)
	}
}

func TestClassifyPublicEventErr_NotFound(t *testing.T) {
	ctx := context.Background()
	got := classifyPublicEventErr(ctx, appErrs.NotFound("acara tidak tersedia"), "t", "e")
	var ee *encoreerrs.Error
	if !errors.As(got, &ee) {
		t.Fatal("expected *errs.Error")
	}
	if ee.Code != encoreerrs.NotFound || ee.Message != msgPublicNotFound {
		t.Fatalf("got code=%v message=%q", ee.Code, ee.Message)
	}
}

func TestClassifyPublicEventErr_InternalSafe(t *testing.T) {
	ctx := context.Background()
	got := classifyPublicEventErr(ctx, errors.New("unexpected boom"), "t", "e")
	var ee *encoreerrs.Error
	if !errors.As(got, &ee) {
		t.Fatal("expected *errs.Error")
	}
	if ee.Code != encoreerrs.Internal || ee.Message != msgPublicInternal {
		t.Fatalf("got code=%v message=%q", ee.Code, ee.Message)
	}
	if strings.Contains(ee.Message, "boom") {
		t.Fatalf("leaked internal detail: %q", ee.Message)
	}
}

func TestClassifyPublicEventErr_PassThroughBadRequest(t *testing.T) {
	ctx := context.Background()
	in := appErrs.BadRequest("nama dan terapi wajib diisi")
	got := classifyPublicEventErr(ctx, in, "t", "e")
	if got != in {
		t.Fatalf("BadRequest should pass through, got %v", got)
	}
}

func TestToPublicPatientScheduleRows(t *testing.T) {
	rows := toPublicPatientScheduleRows([]Patient{
		{FullName: "A", TherapyName: "T1", SlotLabel: "1 Agt", PreferredTime: "09:00", ReservationStatus: "CONFIRMED"},
		{FullName: "B", TherapyName: "T2", SlotLabel: "", PreferredTime: "", ReservationStatus: "PENDING"},
	})
	if len(rows) != 2 {
		t.Fatalf("len = %d", len(rows))
	}
	if rows[0].FullName != "A" || rows[0].TherapyName != "T1" || rows[0].SlotLabel != "1 Agt" || rows[0].PreferredTime != "09:00" {
		t.Fatalf("unexpected row0: %+v", rows[0])
	}
	if rows == nil {
		t.Fatal("nil slice")
	}
	empty := toPublicPatientScheduleRows(nil)
	if empty == nil || len(empty) != 0 {
		t.Fatalf("nil input should yield empty non-nil slice, got %#v", empty)
	}
}

func TestSortPublicPatientScheduleByPreferredTimeASC(t *testing.T) {
	rows := []PublicPatientScheduleRow{
		{FullName: "Candra", PreferredTime: "10:00", SlotLabel: "s3"},
		{FullName: "Ani", PreferredTime: "", SlotLabel: "s0"},
		{FullName: "Budi", PreferredTime: "9:30", SlotLabel: "s2"},
		{FullName: "Dedi", PreferredTime: "09:00", SlotLabel: "s1"},
		{FullName: "Eka", PreferredTime: "  ", SlotLabel: "s4"},
	}
	sortPublicPatientScheduleByPreferredTimeASC(rows)
	want := []string{"Dedi", "Budi", "Candra", "Ani", "Eka"}
	for i, name := range want {
		if rows[i].FullName != name {
			t.Fatalf("index %d: got %q want %q (rows=%+v)", i, rows[i].FullName, name, rows)
		}
	}
}
