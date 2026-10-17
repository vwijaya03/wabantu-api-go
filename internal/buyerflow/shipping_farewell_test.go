package buyerflow

import "testing"

func TestFarewellIsNotShippingQuestion(t *testing.T) {
	for _, s := range []string{"oke kak, sampai besok ya", "makasih, sampai hari ini dulu"} {
		if IsShippingFAQQuestion(s) {
			t.Errorf("IsShippingFAQQuestion(%q) = true, want false (farewell)", s)
		}
	}
	for _, s := range []string{"sampai hari ini atau besok?", "bisa sampai besok kak?"} {
		if !IsShippingFAQQuestion(s) {
			t.Errorf("IsShippingFAQQuestion(%q) = false, want true", s)
		}
	}
}
