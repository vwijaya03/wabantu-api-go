package validate

import (
	"fmt"
	"regexp"
	"strings"
)

var mcqFenceRe = regexp.MustCompile("(?s)```[\\w-]*\\s*([\\s\\S]*?)```")

// MCQReferencesCode reports whether the stem implies a code snippet should be shown.
func MCQReferencesCode(question string) bool {
	lower := strings.ToLower(question)
	for _, phrase := range []string{
		"kode berikut",
		"kode react",
		"perhatikan kode",
		"lihat kode",
		"snippet berikut",
		"cuplikan kode",
		"following code",
		"code below",
		"code snippet",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// MCQInputFromParts builds an MCQInput from bank/exam choice rows.
func MCQInputFromParts(question string, choices []MCQChoice) MCQInput {
	out := make(mcqChoices, len(choices))
	copy(out, choices)
	return MCQInput{Question: question, Choices: out}
}

// NormalizeMCQInput extracts fenced code into code_snippet and trims the intro question.
func NormalizeMCQInput(m *MCQInput) {
	if m == nil {
		return
	}
	if strings.TrimSpace(m.CodeSnippet) == "" {
		if loc := mcqFenceRe.FindStringSubmatchIndex(m.Question); len(loc) >= 4 {
			m.CodeSnippet = strings.TrimSpace(m.Question[loc[2]:loc[3]])
			before := strings.TrimSpace(m.Question[:loc[0]])
			after := strings.TrimSpace(m.Question[loc[1]:])
			m.Question = strings.TrimSpace(strings.TrimRight(before, ":") + " " + after)
		}
	}
	m.Question = strings.TrimSpace(m.Question)
	m.CodeSnippet = strings.TrimSpace(m.CodeSnippet)
}

// NormalizeAndValidateMCQ normalizes display fields then runs schema validation.
func NormalizeAndValidateMCQ(m *MCQInput) error {
	if m == nil {
		return fmt.Errorf("mcq: nil input")
	}
	NormalizeMCQInput(m)
	if MCQReferencesCode(m.Question) && m.CodeSnippet == "" {
		return fmt.Errorf("mcq: code_snippet wajib untuk soal yang meminta analisis kode")
	}
	return ValidateMCQ(m)
}
