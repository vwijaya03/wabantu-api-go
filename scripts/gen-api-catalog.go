//go:build ignore

// gen-api-catalog writes internal/apiregistry/catalog_snapshot.json from //encore:api scan.
//
// Usage (from api-go root):
//
//	go run scripts/gen-api-catalog.go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"encore.app/wabantu/internal/apiregistry"
)

func main() {
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	eps, err := apiregistry.Discover(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out := filepath.Join(root, "internal/apiregistry/catalog_snapshot.json")
	b, err := json.MarshalIndent(struct {
		Count     int                  `json:"count"`
		Endpoints []apiregistry.Endpoint `json:"endpoints"`
	}{Count: len(eps), Endpoints: eps}, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	b = append(b, '\n')
	if err := os.WriteFile(out, b, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%d endpoints)\n", out, len(eps))
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
