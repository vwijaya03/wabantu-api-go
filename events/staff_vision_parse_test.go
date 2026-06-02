package events

import "testing"

func TestParseStaffVisionResponse_commaSeparatedTherapies(t *testing.T) {
	raw := `{"items":[{"fullName":"Bryan","therapyNames":"Terapi 5 Elemen, Terapi Energi Dewa","apakahBisaDatang":"Bisa"}]}`
	items, err := parseStaffVisionResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].FullName != "Bryan" {
		t.Fatalf("name: %q", items[0].FullName)
	}
	if len(items[0].TherapyNames) != 2 {
		t.Fatalf("therapies: %v", items[0].TherapyNames)
	}
	if items[0].AttendanceLabel != "Bisa" {
		t.Fatalf("attendance: %q", items[0].AttendanceLabel)
	}
	if items[0].Role != "terapis" {
		t.Fatalf("role: %q", items[0].Role)
	}
}

func TestParseStaffVisionResponse_indonesianFields(t *testing.T) {
	raw := `{"items":[{"namaTerapis":"Hadi","terapiYangAndaPilih":"Terapi Shijie","kehadiran":"Pagi sampai after lunch"}]}`
	items, err := parseStaffVisionResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].FullName != "Hadi" {
		t.Fatalf("got %+v", items)
	}
	att, notes := mapStaffAttendanceLabel(items[0].AttendanceLabel)
	if att != "PARTIAL" || notes != "Pagi sampai after lunch" {
		t.Fatalf("attendance map: %s %q", att, notes)
	}
}

func TestParseStaffVisionResponse_daoshiPrefix(t *testing.T) {
	raw := `{"items":[{"fullName":"Daoshi David","therapyNames":["Terapi 5 Elemen"]}]}`
	items, err := parseStaffVisionResponse(raw)
	if err != nil || len(items) != 1 || items[0].Role != "daoshi" {
		t.Fatalf("got %+v err=%v", items, err)
	}
}
