package admin

import (
	"context"
	"time"

	bf "encore.app/wabantu/internal/buyerflow"
	"encore.app/wabantu/internal/apiregistry"
	"encore.app/wabantu/shared/retrieval"
)

// AIRegressionSuiteSummary is one package result in the run.
type AIRegressionSuiteSummary struct {
	Name       string `json:"name"`
	Passed     bool   `json:"passed"`
	DurationMs int64  `json:"durationMs"`
	Skipped    bool   `json:"skipped,omitempty"`
	SkipReason string `json:"skipReason,omitempty"`
	Error      string `json:"error,omitempty"`
	CaseCount  int    `json:"caseCount,omitempty"`
	FailedCase string `json:"failedCase,omitempty"`
}

// RunAIRegressionResponse mirrors scripts/run-ai-regression-tests.sh.
type RunAIRegressionResponse struct {
	Passed     bool                       `json:"passed"`
	DurationMs int64                      `json:"durationMs"`
	Suites     []AIRegressionSuiteSummary   `json:"suites"`
	Buyerflow  bf.RegressionRunResult       `json:"buyerflow"`
}

//encore:api auth method=POST path=/api/v1/admin/ai-regression/run tag:super_admin
func RunAIRegression(ctx context.Context) (*RunAIRegressionResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	start := time.Now()

	buyerflow := bf.RunRegressionSuite()
	suites := []AIRegressionSuiteSummary{
		summarizeBuyerflow(buyerflow),
	}

	retrievalRes := retrieval.RunSmokeRegression()
	suites = append(suites, AIRegressionSuiteSummary{
		Name:       "shared/retrieval",
		Passed:     retrievalRes.Passed,
		DurationMs: retrievalRes.DurationMs,
		Error:      retrievalRes.Error,
	})

	apiRes := apiregistry.RunStructuralRegression()
	suites = append(suites, AIRegressionSuiteSummary{
		Name:       "internal/apiregistry",
		Passed:     apiRes.Passed,
		DurationMs: apiRes.DurationMs,
		Skipped:    apiRes.Skipped,
		SkipReason: apiRes.SkipReason,
		Error:      apiRes.Error,
	})

	passed := true
	for _, s := range suites {
		if !s.Skipped && !s.Passed {
			passed = false
			break
		}
	}

	return &RunAIRegressionResponse{
		Passed:     passed,
		DurationMs: time.Since(start).Milliseconds(),
		Suites:     suites,
		Buyerflow:  buyerflow,
	}, nil
}

func summarizeBuyerflow(res bf.RegressionRunResult) AIRegressionSuiteSummary {
	out := AIRegressionSuiteSummary{
		Name:       "internal/buyerflow",
		Passed:     res.Passed,
		DurationMs: res.DurationMs,
	}
	for _, suite := range res.Suites {
		if suite.Skipped {
			continue
		}
		out.CaseCount += len(suite.Cases)
		if !suite.Passed && out.FailedCase == "" {
			for _, c := range suite.Cases {
				if !c.Passed {
					out.FailedCase = c.Name
					out.Error = c.Error
					break
				}
			}
		}
	}
	return out
}
