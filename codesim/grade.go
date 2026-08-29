package codesim

import (
	"context"
	"encoding/json"
	"strings"
)

func gradeSession(ctx context.Context, row *examSessionRow) (*SessionReport, error) {
	earned := 0
	total := 0
	var reportQs []ReportQuestion
	strengths := map[string]int{}
	weaknesses := map[string]int{}

	for _, q := range row.Questions {
		total += q.Points
		rq, ep := gradeQuestion(q, row.Answers)
		earned += ep
		reportQs = append(reportQs, rq)
		topic := q.LearningObjective
		if topic == "" {
			topic = q.Type
		}
		if rq.Correct || rq.Partial {
			strengths[topic]++
		} else {
			weaknesses[topic]++
		}
	}

	norm := 0
	if total > 0 {
		norm = earned * 100 / total
	}
	proctor, _ := countProctorEvents(ctx, row.ID)

	return &SessionReport{
		SessionID:       row.ID,
		EarnedPoints:    earned,
		TotalPoints:     total,
		NormalizedScore: norm,
		Grade:           letterGrade(norm),
		Questions:       reportQs,
		LearningSummary: buildLearningSummary(strengths, weaknesses),
		ProctorSummary:  proctor,
	}, nil
}

func gradeQuestion(q ExamQuestion, answers SessionAnswers) (ReportQuestion, int) {
	switch q.Type {
	case QuestionTypeMCQ:
		return gradeMCQ(q, answers)
	case QuestionTypeReactBuild, QuestionTypeReactDebug:
		return gradeCode(q, answers)
	default:
		return ReportQuestion{
			Index:     q.Index,
			Type:      q.Type,
			MaxPoints: q.Points,
			Prompt:    reportPromptFromQuestion(q),
		}, 0
	}
}

func reportPromptFromQuestion(q ExamQuestion) *ReportQuestionPrompt {
	switch q.Type {
	case QuestionTypeMCQ:
		if q.MCQ == nil {
			return nil
		}
		return &ReportQuestionPrompt{
			Title:       "Multiple Choice",
			Body:        q.MCQ.Question,
			CodeSnippet: q.MCQ.CodeSnippet,
			Choices:     q.MCQ.Choices,
		}
	case QuestionTypeReactBuild:
		if q.Build == nil {
			return nil
		}
		return &ReportQuestionPrompt{
			Title: q.Build.Title,
			Body:  q.Build.SpecMarkdown,
		}
	case QuestionTypeReactDebug:
		if q.Debug == nil {
			return nil
		}
		return &ReportQuestionPrompt{
			Title: q.Debug.Title,
			Body:  q.Debug.BugDescription,
		}
	default:
		return nil
	}
}

func gradeMCQ(q ExamQuestion, answers SessionAnswers) (ReportQuestion, int) {
	var m MCQItem
	_ = json.Unmarshal(q.GradingMeta, &m)
	key := mcqKey(q)
	userAns := ""
	if answers.MCQ != nil {
		userAns = answers.MCQ[key]
	}
	correct := userAns == m.CorrectID
	earned := 0
	label := "Salah"
	if correct {
		earned = q.Points
		label = "Benar"
	}
	feedback := ""
	if !correct && userAns != "" {
		feedback = m.WrongExplanations[userAns]
	}
	return ReportQuestion{
		Index:        q.Index,
		Type:         q.Type,
		Correct:      correct,
		EarnedPoints: earned,
		MaxPoints:    q.Points,
		UserAnswer:   userAns,
		Prompt:       reportPromptFromQuestion(q),
		Debrief: QuestionDebrief{
			ResultLabel:       label,
			Explanation:       m.Explanation,
			AnswerFeedback:    feedback,
			BestPractices:     m.BestPractices,
			LearningObjective: m.LearningObjective,
			CorrectAnswer:     m.CorrectID,
		},
	}, earned
}

func gradeCode(q ExamQuestion, answers SessionAnswers) (ReportQuestion, int) {
	key := codeKey(q)
	ca := CodeAnswer{}
	if answers.Code != nil {
		ca = answers.Code[key]
	}
	earned := 0
	correct := false
	partial := false
	label := "Salah"

	var debrief QuestionDebrief

	switch q.Type {
	case QuestionTypeReactBuild:
		var t BuildTask
		_ = json.Unmarshal(q.GradingMeta, &t)
		if ca.TestsPassed {
			earned = q.Points
			correct = true
			label = "Benar"
		} else if ca.SourceCode != "" {
			label = "Belum lulus test"
		}
		debrief = QuestionDebrief{
			ResultLabel:       label,
			Explanation:       t.SolutionExplanation,
			BestPractices:     t.BestPractices,
			CommonMistakes:    t.CommonMistakes,
			LearningObjective: t.LearningObjective,
			SolutionCode:      t.SolutionCode,
			UserCode:          ca.SourceCode,
		}
		if !correct && ca.SourceCode != "" {
			debrief.AnswerFeedback = "Jalankan test di editor dan pastikan semua kriteria di instruksi terpenuhi — solusi tidak harus sama persis dengan referensi."
		}
	case QuestionTypeReactDebug:
		var t DebugTask
		_ = json.Unmarshal(q.GradingMeta, &t)
		if ca.TestsPassed {
			earned = q.Points
			correct = true
			label = "Benar"
		} else if ca.SourceCode != "" {
			label = "Belum lulus test"
		}
		explanation := t.FixExplanation
		if correct && t.BugDescription != "" {
			explanation = "Gejala: " + t.BugDescription + "\n\nPerbaikan: " + t.FixExplanation
		}
		debrief = QuestionDebrief{
			ResultLabel:       label,
			Explanation:       explanation,
			BestPractices:     t.BestPractices,
			CommonMistakes:    t.CommonMistakes,
			LearningObjective: t.LearningObjective,
			SolutionCode:      t.SolutionCode,
			UserCode:          ca.SourceCode,
		}
		if !correct {
			debrief.AnswerFeedback = t.RootCause
		}
	}

	return ReportQuestion{
		Index:        q.Index,
		Type:         q.Type,
		Correct:      correct,
		Partial:      partial,
		EarnedPoints: earned,
		MaxPoints:    q.Points,
		Prompt:       reportPromptFromQuestion(q),
		Debrief:      debrief,
	}, earned
}

func letterGrade(score int) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 80:
		return "B"
	case score >= 70:
		return "C"
	case score >= 60:
		return "D"
	default:
		return "F"
	}
}

func buildLearningSummary(strengths, weaknesses map[string]int) LearningSummary {
	var s, w, rec []string
	for k, v := range strengths {
		if v > 0 {
			s = append(s, k)
		}
	}
	for k, v := range weaknesses {
		if v > 0 {
			w = append(w, k)
			rec = append(rec, "Ulangi: "+k)
		}
	}
	if len(s) == 0 && len(w) == 0 {
		s = []string{"Dasar frontend"}
	}
	return LearningSummary{
		Strengths:         s,
		Weaknesses:        w,
		RecommendedTopics: rec,
	}
}

func normalizeTopic(s string) string {
	return strings.TrimSpace(s)
}
