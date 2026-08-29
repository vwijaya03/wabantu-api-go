package codesim

import "testing"

func TestCloneExamQuestions(t *testing.T) {
	src := []ExamQuestion{{
		Index:    1,
		Type:     QuestionTypeMCQ,
		SourceID: "ai-mcq-1",
		Points:   10,
		MCQ:      &ExamMCQ{Question: "Q?", CodeSnippet: "const x=1"},
	}}
	out, err := cloneExamQuestions(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].MCQ == nil || out[0].MCQ.CodeSnippet != "const x=1" {
		t.Fatalf("unexpected clone: %+v", out)
	}
	out[0].MCQ.CodeSnippet = "mutated"
	if src[0].MCQ.CodeSnippet != "const x=1" {
		t.Fatal("clone should be deep copy")
	}
}
