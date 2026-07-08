package events

import "testing"

func TestResolvePatientOrderByDefault(t *testing.T) {
	got, err := resolvePatientOrderBy("", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != patientOrderBy {
		t.Fatalf("expected default order, got %q", got)
	}
}

func TestResolvePatientOrderByInvalid(t *testing.T) {
	_, err := resolvePatientOrderBy("invalid", "asc")
	if err == nil {
		t.Fatal("expected error for invalid sortBy")
	}
}

func TestSortPatientsInMemoryByName(t *testing.T) {
	items := []Patient{
		{FullName: "Zara"},
		{FullName: "Anto"},
		{FullName: "Budi"},
	}
	sortPatientsInMemory(items, "name", "asc")
	if items[0].FullName != "Anto" || items[2].FullName != "Zara" {
		t.Fatalf("unexpected asc order: %+v", items)
	}
	sortPatientsInMemory(items, "name", "desc")
	if items[0].FullName != "Zara" {
		t.Fatalf("unexpected desc order: %+v", items)
	}
}

func TestResolveEventOrderBy(t *testing.T) {
	got, err := resolveEventOrderBy("eventName", "asc")
	if err != nil {
		t.Fatal(err)
	}
	if got != "ORDER BY event_name ASC, start_date DESC" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestSortStaffListRowsInMemory(t *testing.T) {
	rows := []staffListRow{
		{FullName: "Zara", PersonType: "VOLUNTEER"},
		{FullName: "Anto", PersonType: "THERAPIST"},
	}
	sortStaffListRowsInMemory(rows, "name", "asc")
	if rows[0].FullName != "Anto" {
		t.Fatalf("unexpected: %+v", rows)
	}
}
