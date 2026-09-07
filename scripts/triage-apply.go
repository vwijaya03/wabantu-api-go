// Command triage-apply writes internal/buyerflow/regression_autogen_<job>_test.go from stdin.
//
// Usage:
//
//	go run scripts/triage-apply.go --job-id <uuid> < regression_snippet.go
//
// Stdin must contain the output of ai.GenerateRegressionCases (package buyerflow + cases function).
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"encore.app/wabantu/internal/triageautogen"
)

func main() {
	jobID := flag.String("job-id", "", "ai_triage_job id")
	outPath := flag.String("out", "", "output path relative to api-go root (default: per-job file)")
	flag.Parse()

	if strings.TrimSpace(*jobID) == "" {
		fatal("job-id required")
	}

	root, err := repoRoot()
	if err != nil {
		fatal(err.Error())
	}

	body, err := io.ReadAll(os.Stdin)
	if err != nil {
		fatal(err.Error())
	}
	content := strings.TrimSpace(string(body))
	if content == "" {
		fatal("stdin empty: regression cases required")
	}

	rel := strings.TrimSpace(*outPath)
	if rel == "" {
		rel = triageautogen.AutoGenRelPath(*jobID)
	}
	target := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		fatal(err.Error())
	}
	file := triageautogen.BuildAutoGenTestFile(*jobID, content)
	if err := os.WriteFile(target, []byte(file), 0o644); err != nil {
		fatal(err.Error())
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", target)
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "encore.app")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("encore.app not found from cwd")
		}
		dir = parent
	}
}

func fatal(msg string) {
	fmt.Fprintf(os.Stderr, "triage-apply: %s\n", msg)
	os.Exit(1)
}
