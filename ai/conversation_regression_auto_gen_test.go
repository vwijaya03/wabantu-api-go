package ai

import "testing"

// Baseline: no auto-generated cases until AI triage workflow writes this file.
func conversationRegressionAutoGenCases() []conversationRegressionCase {
	return nil
}

func TestConversationRegressionAutoGen(t *testing.T) {
	cases := conversationRegressionAutoGenCases()
	if len(cases) == 0 {
		t.Skip("no auto-generated regression cases")
	}
	sim := newOmahSimulator()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			local := newOmahSimulator()
			local.History = append([]dbMessage{}, sim.History...)
			local.Order = sim.Order
			out := local.Turn(tc.input)
			if tc.extraCheck != nil {
				tc.extraCheck(t, out)
				return
			}
			if tc.wantPath != "" && out.Path != tc.wantPath {
				t.Fatalf("path = %q want %q reply=%q", out.Path, tc.wantPath, out.Reply)
			}
		})
	}
}
