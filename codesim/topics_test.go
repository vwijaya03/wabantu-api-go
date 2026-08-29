package codesim

import "testing"

func TestMergeLearnerMCQFilters_keepsBuildDebug(t *testing.T) {
	cfg := DefaultBlueprintConfig()
	buildFamily := ""
	debugFamily := ""
	for _, sec := range cfg.Sections {
		if sec.Type == QuestionTypeReactBuild {
			buildFamily = sec.ComponentFamily
		}
		if sec.Type == QuestionTypeReactDebug {
			debugFamily = sec.ComponentFamily
		}
	}

	out := MergeLearnerMCQFilters(cfg, []string{"react"}, "medium", 0)

	var mcqTags []string
	var mcqDiff string
	var mcqCount int
	for _, sec := range out.Sections {
		switch sec.Type {
		case QuestionTypeMCQ:
			mcqTags = sec.Tags
			mcqDiff = sec.Difficulty
			mcqCount = sec.Count
		case QuestionTypeReactBuild:
			if sec.ComponentFamily != buildFamily {
				t.Fatalf("build family changed: %q -> %q", buildFamily, sec.ComponentFamily)
			}
		case QuestionTypeReactDebug:
			if sec.ComponentFamily != debugFamily {
				t.Fatalf("debug family changed: %q -> %q", debugFamily, sec.ComponentFamily)
			}
		}
	}
	if len(mcqTags) != 1 || mcqTags[0] != "react" {
		t.Fatalf("tags = %v", mcqTags)
	}
	if mcqDiff != "medium" {
		t.Fatalf("difficulty = %q", mcqDiff)
	}
	if mcqCount != 5 {
		t.Fatalf("mcq count = %d", mcqCount)
	}
	if out.TotalTimeLimitMinutes != 98 {
		t.Fatalf("duration = %d", out.TotalTimeLimitMinutes)
	}
}

func TestTendemExamFormat(t *testing.T) {
	f := TendemExamFormat()
	if f.TotalQuestions != 7 || f.DurationMinutes != 98 {
		t.Fatalf("format = %+v", f)
	}
	if len(f.Sections) != 3 {
		t.Fatalf("sections = %d", len(f.Sections))
	}
}
