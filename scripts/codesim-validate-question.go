// Validate codesim question JSON from stdin.
//
// Usage:
//
//	go run scripts/codesim-validate-question.go < question.json
//	go run scripts/codesim-validate-question.go -type=mcq < question.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"encore.app/wabantu/codesim/validate"
)

func main() {
	typ := flag.String("type", "", "mcq|build|debug (auto-detect if empty)")
	flag.Parse()

	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	kind := *typ
	if kind == "" {
		kind = detectType(raw)
	}

	switch kind {
	case "mcq":
		if raw[0] == '[' {
			var items []validate.MCQInput
			if err := json.Unmarshal(raw, &items); err != nil {
				fail(err)
			}
			for i, m := range items {
				if err := validate.ValidateMCQ(&m); err != nil {
					fail(fmt.Errorf("item %d: %w", i, err))
				}
			}
			break
		}
		var m validate.MCQInput
		if err := json.Unmarshal(raw, &m); err != nil {
			fail(err)
		}
		if err := validate.ValidateMCQ(&m); err != nil {
			fail(err)
		}
	case "build":
		if raw[0] == '[' {
			var items []validate.BuildInput
			if err := json.Unmarshal(raw, &items); err != nil {
				fail(err)
			}
			for i, b := range items {
				if err := validate.ValidateBuild(&b); err != nil {
					fail(fmt.Errorf("item %d: %w", i, err))
				}
			}
			break
		}
		var b validate.BuildInput
		if err := json.Unmarshal(raw, &b); err != nil {
			fail(err)
		}
		if err := validate.ValidateBuild(&b); err != nil {
			fail(err)
		}
	case "debug":
		if raw[0] == '[' {
			var items []validate.DebugInput
			if err := json.Unmarshal(raw, &items); err != nil {
				fail(err)
			}
			for i, d := range items {
				if err := validate.ValidateDebug(&d); err != nil {
					fail(fmt.Errorf("item %d: %w", i, err))
				}
			}
			break
		}
		var d validate.DebugInput
		if err := json.Unmarshal(raw, &d); err != nil {
			fail(err)
		}
		if err := validate.ValidateDebug(&d); err != nil {
			fail(err)
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown type; use -type=mcq|build|debug or include discriminating fields")
		os.Exit(1)
	}

	fmt.Println("OK")
}

func detectType(raw []byte) string {
	raw = bytesTrimSpace(raw)
	if len(raw) > 0 && raw[0] == '[' {
		var arr []json.RawMessage
		if err := json.Unmarshal(raw, &arr); err != nil || len(arr) == 0 {
			return ""
		}
		return detectType(arr[0])
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		fail(err)
	}
	if _, ok := probe["broken_code"]; ok {
		return "debug"
	}
	if _, ok := probe["starter_code"]; ok {
		return "build"
	}
	if _, ok := probe["choices"]; ok {
		return "mcq"
	}
	return ""
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func bytesTrimSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\n' || b[0] == '\r' || b[0] == '\t') {
		b = b[1:]
	}
	return b
}
