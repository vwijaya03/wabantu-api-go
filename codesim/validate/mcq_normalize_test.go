package validate

import (
	"strings"
	"testing"
)

func TestNormalizeMCQInput_extractsFence(t *testing.T) {
	m := MCQInput{
		Question: "Apa output?\n```javascript\nconst x = 1;\n```\nPilih jawaban.",
	}
	NormalizeMCQInput(&m)
	if m.CodeSnippet != "const x = 1;" {
		t.Fatalf("snippet=%q", m.CodeSnippet)
	}
	if m.Question == "" || strings.Contains(m.Question, "```") {
		t.Fatalf("question should be intro only: %q", m.Question)
	}
}

func TestNormalizeAndValidateMCQ_requiresSnippet(t *testing.T) {
	m := MCQInput{
		Question: "Perhatikan kode React berikut. Apa yang terjadi?",
		Choices: mcqChoices{
			{ID: "a", Text: "A"},
			{ID: "b", Text: "B"},
			{ID: "c", Text: "C"},
			{ID: "d", Text: "D"},
		},
		CorrectID:         "a",
		Explanation:       "karena",
		WrongExplanations: map[string]string{"b": "x", "c": "x", "d": "x"},
		BestPractices:     []string{"a", "b"},
		Points:            10,
	}
	if err := NormalizeAndValidateMCQ(&m); err == nil {
		t.Fatal("expected error without code_snippet")
	}
	m.CodeSnippet = "export function App(){ return null; }"
	if err := NormalizeAndValidateMCQ(&m); err != nil {
		t.Fatal(err)
	}
}
