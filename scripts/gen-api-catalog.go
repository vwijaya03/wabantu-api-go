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
	"sort"

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

	countsPath := filepath.Join(root, "internal/apiregistry/service_counts.json")
	serviceCounts := make(map[string]int)
	for _, ep := range eps {
		serviceCounts[ep.Service]++
	}
	cb, err := json.MarshalIndent(struct {
		TotalServices  int            `json:"total_services"`
		TotalEndpoints int            `json:"total_endpoints"`
		Services       map[string]int `json:"services"`
	}{
		TotalServices:  len(serviceCounts),
		TotalEndpoints: len(eps),
		Services:       serviceCounts,
	}, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cb = append(cb, '\n')
	if err := os.WriteFile(countsPath, cb, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	services := make([]string, 0, len(serviceCounts))
	for svc := range serviceCounts {
		services = append(services, svc)
	}
	sort.Strings(services)
	fmt.Fprintf(os.Stderr, "wrote %s (%d services)\n", countsPath, len(services))
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
