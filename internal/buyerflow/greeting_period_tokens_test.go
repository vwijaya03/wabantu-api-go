package buyerflow

import (
	"strings"
	"testing"
)

func TestGreetingPeriodWordsAnywhere(t *testing.T) {
	cases := map[string]GreetingPeriod{"Hello morning": GreetMorning, "Hello evening": GreetEvening, "hi good afternoon": GreetAfternoon, "halo kak selamat malam": GreetNight}
	for msg, want := range cases {
		got, ok := DetectGreetingPeriodFromText(msg)
		if !ok || got != want {
			t.Errorf("DetectGreetingPeriodFromText(%q) = (%q,%v), want %q", msg, got, ok, want)
		}
	}
	if _, ok := DetectGreetingPeriodFromText("Hello"); ok {
		t.Error("bare hello has no period")
	}
}

func TestGreetingReplyMirrorsBuyerTimeWord(t *testing.T) {
	cases := map[string]string{
		"Hello morning": "Good morning",
		"Hello evening": "Good evening",
		"halo kak sore": "Selamat sore",
		"hai kak pagi":  "Selamat pagi",
	}
	for msg, want := range cases {
		if got := GreetingReply(msg, "friendly", ""); !strings.Contains(got, want) {
			t.Errorf("GreetingReply(%q) = %q, want it to contain %q", msg, got, want)
		}
	}
	// Through the simulator the greeting path must give the same answer, whatever the clock says.
	sim := newOmahSteakSimulator()
	out := sim.Turn("Hello evening")
	if out.Path != PathGreeting || !strings.Contains(out.Reply, "Good evening") {
		t.Errorf("simulator: path=%s reply=%q, want a Good evening greeting", out.Path, out.Reply)
	}
}
