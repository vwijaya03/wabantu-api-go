package events

import "testing"

func TestBuildDaySlotsWithBreak_lunchGap(t *testing.T) {
	bs := "11:30:00"
	be := "13:00:00"
	slots, err := buildDaySlotsWithBreak("09:00", "17:00", 30, &bs, &be)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(slots) == 0 {
		t.Fatal("expected slots")
	}
	lastMorning := slots[4]
	if lastMorning.start != "11:00:00" || lastMorning.end != "11:30:00" {
		t.Fatalf("last morning slot = %+v, want 11:00-11:30", lastMorning)
	}
	firstAfternoon := slots[5]
	if firstAfternoon.start != "13:00:00" || firstAfternoon.end != "13:30:00" {
		t.Fatalf("first afternoon slot = %+v, want 13:00-13:30", firstAfternoon)
	}
	for _, sl := range slots {
		if sl.start >= "11:30:00" && sl.start < "13:00:00" {
			t.Fatalf("slot inside break window: %+v", sl)
		}
	}
}

func TestBuildDaySlotsWithBreak_noBreak(t *testing.T) {
	withBreak, err := buildDaySlotsWithBreak("09:00", "12:00", 30, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := buildDaySlots("09:00", "12:00", 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(withBreak) != len(plain) {
		t.Fatalf("len withBreak=%d plain=%d", len(withBreak), len(plain))
	}
	for i := range plain {
		if withBreak[i] != plain[i] {
			t.Fatalf("slot %d differs: %+v vs %+v", i, withBreak[i], plain[i])
		}
	}
}

func TestDayScheduleSegments_breakOutsideRangeIgnored(t *testing.T) {
	bs := "18:00:00"
	be := "19:00:00"
	segments, err := dayScheduleSegments("09:00", "17:00", &bs, &be)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 1 || segments[0].start != "09:00" || segments[0].end != "17:00" {
		t.Fatalf("unexpected segments: %+v", segments)
	}
}
