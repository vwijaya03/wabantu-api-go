package apiregistry

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Endpoint — satu baris //encore:api di codebase.
type Endpoint struct {
	Service    string `json:"service"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Access     string `json:"access"` // auth | public | private
	Method     string `json:"method"` // GET, POST, *, raw, ...
	Path       string `json:"path"`
	Raw        bool   `json:"raw,omitempty"`
	Tag        string `json:"tag,omitempty"`
	Annotation string `json:"annotation"`
}

var encoreAPIRe = regexp.MustCompile(`//encore:api\s+(.+)`)

// Discover walks api-go root and parses all //encore:api annotations.
func Discover(root string) ([]Endpoint, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("apiregistry: empty root")
	}
	var out []Endpoint
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			if base == ".git" || base == "vendor" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		service := strings.Split(rel, string(os.PathSeparator))[0]
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		lineNo := 0
		for sc.Scan() {
			lineNo++
			line := strings.TrimSpace(sc.Text())
			if !strings.HasPrefix(line, "//encore:api ") {
				continue
			}
			m := encoreAPIRe.FindStringSubmatch(line)
			if len(m) < 2 {
				continue
			}
			ep, perr := parseAnnotation(service, rel, lineNo, m[1])
			if perr != nil {
				return fmt.Errorf("%s:%d: %w", rel, lineNo, perr)
			}
			out = append(out, ep)
		}
		return sc.Err()
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		if out[i].Method != out[j].Method {
			return out[i].Method < out[j].Method
		}
		return out[i].File < out[j].File
	})
	return out, nil
}

func parseAnnotation(service, file string, line int, body string) (Endpoint, error) {
	ep := Endpoint{
		Service:    service,
		File:       filepath.ToSlash(file),
		Line:       line,
		Annotation: strings.TrimSpace(body),
	}
	tokens := strings.Fields(body)
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		switch {
		case tok == "raw":
			ep.Raw = true
		case strings.HasPrefix(tok, "tag:"):
			ep.Tag = strings.TrimPrefix(tok, "tag:")
		case tok == "auth" || tok == "public" || tok == "private":
			ep.Access = tok
		case strings.HasPrefix(tok, "method="):
			ep.Method = strings.ToUpper(strings.TrimPrefix(tok, "method="))
		case strings.HasPrefix(tok, "path="):
			ep.Path = strings.TrimPrefix(tok, "path=")
		case tok == "path" && i+1 < len(tokens):
			i++
			ep.Path = tokens[i]
		}
	}
	if ep.Path == "" {
		return Endpoint{}, fmt.Errorf("missing path in %q", body)
	}
	if !strings.HasPrefix(ep.Path, "/") {
		ep.Path = "/" + ep.Path
	}
	if ep.Method == "" {
		if ep.Raw {
			ep.Method = "*"
		} else {
			return Endpoint{}, fmt.Errorf("missing method in %q", body)
		}
	}
	if ep.Access == "" {
		// raw webhooks without explicit access are public in practice
		ep.Access = "public"
	}
	return ep, nil
}

// RouteKey — unique identity for method+path (Encore may register multiple HTTP verbs on raw).
func RouteKey(method, path string) string {
	return strings.ToUpper(method) + " " + path
}
