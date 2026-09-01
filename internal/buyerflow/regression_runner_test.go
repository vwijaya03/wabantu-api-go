package buyerflow

import "testing"

func TestRunRegressionSuitePasses(t *testing.T) {
	res := RunRegressionSuite()
	if !res.Passed {
		for _, suite := range res.Suites {
			if suite.Skipped {
				continue
			}
			for _, c := range suite.Cases {
				if !c.Passed {
					t.Fatalf("suite %s case %s: %s", suite.Name, c.Name, c.Error)
				}
			}
		}
		t.Fatal("RunRegressionSuite returned passed=false without case error")
	}
}
