// Validate all embedded codesim seed files (no DB required).
//
// Usage:
//
//	go run scripts/codesim-import-seed.go
//
// To load into local DB, start encore and hit any codesim endpoint (EnsureSeed on first use),
// or use migration + encore test harness.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"encore.app/wabantu/codesim/validate"
)

func main() {
	root := filepath.Join("codesim", "seed")
	files := map[string]string{
		filepath.Join(root, "mcq.json"):              "mcq",
		filepath.Join(root, "build.json"):            "build",
		filepath.Join(root, "debug.json"):            "debug",
		filepath.Join(root, "tendem_mcq.json"):       "mcq",
		filepath.Join(root, "tendem_build.json"):     "build",
		filepath.Join(root, "tendem_debug.json"):     "debug",
		filepath.Join(root, "tendem_blueprints.json"): "blueprints",
	}

	for path, kind := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, path, err)
			os.Exit(1)
		}
		if err := validateFile(kind, raw); err != nil {
			fmt.Fprintln(os.Stderr, path+":", err)
			os.Exit(1)
		}
		fmt.Println(path, "OK")
	}
	fmt.Println("all seed files valid")
}

func validateFile(kind string, raw []byte) error {
	switch kind {
	case "mcq":
		var items []validate.MCQInput
		if err := json.Unmarshal(raw, &items); err != nil {
			return err
		}
		for i, it := range items {
			if err := validate.ValidateMCQ(&it); err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
		}
	case "build":
		var items []validate.BuildInput
		if err := json.Unmarshal(raw, &items); err != nil {
			return err
		}
		for i, it := range items {
			if err := validate.ValidateBuild(&it); err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
		}
	case "debug":
		var items []validate.DebugInput
		if err := json.Unmarshal(raw, &items); err != nil {
			return err
		}
		for i, it := range items {
			if err := validate.ValidateDebug(&it); err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
		}
	case "blueprints":
		var items []struct {
			Slug   string          `json:"slug"`
			Title  string          `json:"title"`
			Config json.RawMessage `json:"config"`
		}
		if err := json.Unmarshal(raw, &items); err != nil {
			return err
		}
		for i, it := range items {
			if it.Slug == "" || it.Title == "" || len(it.Config) == 0 {
				return fmt.Errorf("item %d: slug, title, config required", i)
			}
		}
	default:
		return fmt.Errorf("unknown kind %q", kind)
	}
	return nil
}
