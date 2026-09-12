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
	jobID := flag.String("job-id", "", "ai_triage_behavior_job id")
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
		fatal("stdin empty")
	}
	rel := triageautogen.BehaviorRelPath(*jobID)
	target := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		fatal(err.Error())
	}
	if err := os.WriteFile(target, []byte(content+"\n"), 0o644); err != nil {
		fatal(err.Error())
	}
	fmt.Fprintf(os.Stderr, "wrote %s hash=%s\n", rel, triageautogen.HashGeneratedFile(content+"\n"))
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
			return "", fmt.Errorf("encore.app not found")
		}
		dir = parent
	}
}

func fatal(msg string) {
	fmt.Fprintf(os.Stderr, "triage-behavior-apply: %s\n", msg)
	os.Exit(1)
}
