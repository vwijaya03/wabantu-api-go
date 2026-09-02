package buyerflow

import "testing"

func TestGreetingLocaleEN(t *testing.T) {
	if !isPureGreetingCore("good evening") {
		t.Fatal("good evening should be greeting")
	}
	period, ok := DetectGreetingPeriodFromText("good evening")
	if !ok || period != GreetEvening {
		t.Fatalf("period=%v ok=%v", period, ok)
	}
}

func TestGreetingLocaleJawa(t *testing.T) {
	if !isPureGreetingCore("sugeng enjong") {
		t.Fatal("sugeng enjong should be greeting")
	}
	if !isPureGreetingCore("sugeng rawuh") {
		t.Fatal("sugeng rawuh should be greeting")
	}
}
