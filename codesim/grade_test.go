package codesim

import (
	"encoding/json"
	"testing"
)

func TestReportPromptFromQuestion_mcq(t *testing.T) {
	q := ExamQuestion{
		Type: QuestionTypeMCQ,
		MCQ: &ExamMCQ{
			Question: "Apa itu useState?",
			Choices:  []MCQChoice{{ID: "a", Text: "Hook"}},
		},
	}
	p := reportPromptFromQuestion(q)
	if p == nil || p.Body != "Apa itu useState?" || len(p.Choices) != 1 {
		t.Fatalf("prompt = %+v", p)
	}
}

func TestReportPromptFromQuestion_build(t *testing.T) {
	q := ExamQuestion{
		Type: QuestionTypeReactBuild,
		Build: &ExamBuild{
			Title:        "CommentForm",
			SpecMarkdown: "## Tujuan\nBuat form",
		},
	}
	p := reportPromptFromQuestion(q)
	if p == nil || p.Title != "CommentForm" || p.Body == "" {
		t.Fatalf("prompt = %+v", p)
	}
}

func TestGradeCode_debugCorrectOmitsWrongFeedback(t *testing.T) {
	meta, _ := json.Marshal(DebugTask{
		Title:          "Hero Missing Key #21",
		BugDescription: "List CTA salah urutan setelah filter.",
		RootCause:      "Index sebagai key menyebabkan reconciler salah reuse DOM.",
		FixExplanation: "Gunakan id stabil dari data sebagai key.",
		SolutionCode:   "export function Hero(){}",
	})
	q := ExamQuestion{
		Index:       7,
		Type:        QuestionTypeReactDebug,
		Points:      35,
		GradingMeta: meta,
	}
	answers := SessionAnswers{
		Code: map[string]CodeAnswer{
			"7": {SourceCode: "fixed", TestsPassed: true},
		},
	}
	rq, earned := gradeQuestion(q, answers)
	if !rq.Correct || earned != 35 {
		t.Fatalf("expected full credit, got %d correct=%v", earned, rq.Correct)
	}
	if rq.Debrief.AnswerFeedback != "" {
		t.Fatalf("correct answer should not have wrong-feedback: %q", rq.Debrief.AnswerFeedback)
	}
}

func TestGradeCode_buildFullCreditOnTestsPassed(t *testing.T) {
	meta, _ := json.Marshal(BuildTask{
		Title:               "CommentForm",
		SolutionExplanation: "Controlled input + validasi",
		SolutionCode:        "export function CommentForm(){}",
		Rubric: Rubric{
			Criteria: []RubricCriterion{{ID: "tests_pass", Points: 40, Auto: true}},
		},
	})
	q := ExamQuestion{
		Index:       6,
		Type:        QuestionTypeReactBuild,
		Points:      40,
		GradingMeta: meta,
	}
	answers := SessionAnswers{
		Code: map[string]CodeAnswer{
			"6": {SourceCode: "user code", TestsPassed: true},
		},
	}
	rq, earned := gradeQuestion(q, answers)
	if !rq.Correct || earned != 40 || rq.Partial {
		t.Fatalf("expected 40/40 full credit, got %d correct=%v partial=%v", earned, rq.Correct, rq.Partial)
	}
}
