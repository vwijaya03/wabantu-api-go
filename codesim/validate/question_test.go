package validate

import (
	"encoding/json"
	"testing"
)

func TestValidateMCQ_ok(t *testing.T) {
	m := &MCQInput{
		Question:    "Q?",
		CorrectID:   "b",
		Explanation: "because",
		Points:      10,
		Choices: []MCQChoice{
			{ID: "a", Text: "A"},
			{ID: "b", Text: "B"},
			{ID: "c", Text: "C"},
			{ID: "d", Text: "D"},
		},
		WrongExplanations: map[string]string{"a": "no", "c": "no", "d": "no"},
		BestPractices:     []string{"one", "two"},
	}
	if err := ValidateMCQ(m); err != nil {
		t.Fatalf("expected ok: %v", err)
	}
}

func TestValidateMCQ_missingWrongExplanation(t *testing.T) {
	m := &MCQInput{
		Question:    "Q?",
		CorrectID:   "b",
		Explanation: "because",
		Points:      10,
		Choices: []MCQChoice{
			{ID: "a", Text: "A"},
			{ID: "b", Text: "B"},
			{ID: "c", Text: "C"},
			{ID: "d", Text: "D"},
		},
		WrongExplanations: map[string]string{"a": "no"},
		BestPractices:     []string{"one", "two"},
	}
	if err := ValidateMCQ(m); err == nil {
		t.Fatal("expected error for missing wrong_explanations")
	}
}

func TestValidateBuild_ok(t *testing.T) {
	rubric := json.RawMessage(`{"criteria":[{"id":"tests_pass"},{"id":"a11y"},{"id":"validation"}]}`)
	b := &BuildInput{
		Title:               "WaitlistForm",
		StarterCode:         "export {}",
		SolutionCode:        "export {}",
		SolutionExplanation: "steps",
		RubricJSON:          rubric,
		BestPractices:       []string{"a", "b", "c"},
		CommonMistakes:      []string{"x", "y"},
		Points:              40,
	}
	if err := ValidateBuild(b); err != nil {
		t.Fatalf("expected ok: %v", err)
	}
}

func TestMCQChoices_unmarshalObjectMap(t *testing.T) {
	var m MCQInput
	raw := `{"question":"Q?","choices":{"a":"Opsi A","b":"Opsi B","c":"Opsi C","d":"Opsi D"},"correct_id":"b","explanation":"karena","wrong_explanations":{"a":"salah","c":"salah","d":"salah"},"best_practices":["a","b"],"points":10}`
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Choices) != 4 {
		t.Fatalf("choices len=%d", len(m.Choices))
	}
	if m.Choices[0].ID != "a" || m.Choices[0].Text != "Opsi A" {
		t.Fatalf("first choice: %+v", m.Choices[0])
	}
}
