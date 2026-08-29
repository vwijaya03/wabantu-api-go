package codesim

import (
	"math/rand"
	"testing"
)

func TestPickMCQs_deterministic(t *testing.T) {
	all := make([]MCQItem, 10)
	for i := range all {
		all[i] = MCQItem{
			ID:       string(rune('a' + i)),
			Tags:     []string{"react"},
			Points:   10,
			CorrectID: "a",
		}
	}
	rng := rand.New(rand.NewSource(99))
	p1, err := pickMCQs(all, []string{"react"}, "", 5, rng)
	if err != nil {
		t.Fatal(err)
	}
	rng2 := rand.New(rand.NewSource(99))
	p2, err := pickMCQs(all, []string{"react"}, "", 5, rng2)
	if err != nil {
		t.Fatal(err)
	}
	for i := range p1 {
		if p1[i].ID != p2[i].ID {
			t.Fatalf("seed mismatch at %d", i)
		}
	}
}

func TestLetterGrade(t *testing.T) {
	if letterGrade(95) != "A" || letterGrade(55) != "F" {
		t.Fatal("grade mismatch")
	}
}

func TestNewRandomSeed_unique(t *testing.T) {
	a := newRandomSeed()
	b := newRandomSeed()
	if a == b {
		t.Fatal("expected different seeds")
	}
}
