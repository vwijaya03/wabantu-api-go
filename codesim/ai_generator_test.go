package codesim

import (
	"encoding/json"
	"testing"
)

func TestSplitMCQGen(t *testing.T) {
	t.Parallel()
	cases := []struct {
		total int
		want  []int // counts per batch
	}{
		{3, []int{3}},
		{4, []int{2, 2}},
		{5, []int{3, 2}},
		{7, []int{4, 3}},
	}
	for _, tc := range cases {
		batches := splitMCQGen(tc.total)
		if len(batches) != len(tc.want) {
			t.Fatalf("total=%d: got %d batches, want %d", tc.total, len(batches), len(tc.want))
		}
		sum := 0
		for i, b := range batches {
			if b.Count != tc.want[i] {
				t.Fatalf("total=%d batch %d: count=%d want=%d", tc.total, i, b.Count, tc.want[i])
			}
			sum += b.Count
		}
		if sum != tc.total {
			t.Fatalf("total=%d: batch sum=%d", tc.total, sum)
		}
	}
}

func TestExtractJSONObject(t *testing.T) {
	t.Parallel()
	raw := "```json\n{\"mcqs\":[{\"question\":\"q\"}]}\n```"
	got := extractJSONObject(raw)
	if !json.Valid([]byte(got)) {
		t.Fatalf("invalid json: %q", got)
	}
}

func TestExtractJSONObject_nestedBraces(t *testing.T) {
	t.Parallel()
	raw := `prefix {"build":{"starter_code":"export function X(){return <div/>;}"}} suffix`
	got := extractJSONObject(raw)
	var out struct {
		Build struct {
			StarterCode string `json:"starter_code"`
		} `json:"build"`
	}
	if err := json.Unmarshal([]byte(got), &out); err != nil {
		t.Fatal(err)
	}
	if out.Build.StarterCode == "" {
		t.Fatal("expected starter_code")
	}
}
