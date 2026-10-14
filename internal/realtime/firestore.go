package realtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Writer upserts (merge) one document. Implementations: RESTWriter, fakes in tests.
type Writer interface {
	Set(ctx context.Context, docPath string, data map[string]any) error
}

// RESTWriter writes through the Firestore REST API (documents.patch with an update mask,
// i.e. merge semantics; the document is created when missing).
type RESTWriter struct {
	BaseURL string // default https://firestore.googleapis.com/v1
	Project string
	Token   func(ctx context.Context) (string, error)
	HTTP    *http.Client
}

func NewRESTWriter(cfg *Config) *RESTWriter {
	hc := &http.Client{Timeout: 5 * time.Second}
	ts := &tokenSource{cfg: cfg, tokenURL: googleTokenURL, hc: hc}
	return &RESTWriter{BaseURL: "https://firestore.googleapis.com/v1", Project: cfg.ProjectID, Token: ts.Token, HTTP: hc}
}

func (w *RESTWriter) Set(ctx context.Context, docPath string, data map[string]any) error {
	if err := validDocPath(docPath); err != nil {
		return err
	}
	fields := map[string]any{}
	q := url.Values{}
	for k, v := range data {
		fields[k] = encodeValue(v)
		q.Add("updateMask.fieldPaths", "`"+strings.ReplaceAll(k, "`", "")+"`")
	}
	body, _ := json.Marshal(map[string]any{"fields": fields})
	u := fmt.Sprintf("%s/projects/%s/databases/(default)/documents/%s?%s",
		strings.TrimRight(w.BaseURL, "/"), url.PathEscape(w.Project), docPath, q.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if w.Token != nil {
		tok, err := w.Token(ctx)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := w.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("firestore: http %d: %.200s", resp.StatusCode, b)
	}
	return nil
}

// validDocPath requires an even number of non-empty, slash-free segments of safe chars.
func validDocPath(p string) error {
	segs := strings.Split(p, "/")
	if len(segs) < 2 || len(segs)%2 != 0 {
		return fmt.Errorf("firestore: invalid document path %q", p)
	}
	for _, s := range segs {
		if s == "" || s == "." || s == ".." || len(s) > 200 {
			return fmt.Errorf("firestore: invalid document path %q", p)
		}
		for _, r := range s {
			if !(r == '-' || r == '_' || r == '.' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
				return fmt.Errorf("firestore: invalid document path %q", p)
			}
		}
	}
	return nil
}

// encodeValue converts Go values (incl. JSON-roundtripped structs) to Firestore Values.
func encodeValue(v any) map[string]any {
	switch x := v.(type) {
	case nil:
		return map[string]any{"nullValue": nil}
	case string:
		return map[string]any{"stringValue": x}
	case bool:
		return map[string]any{"booleanValue": x}
	case int:
		return map[string]any{"integerValue": fmt.Sprint(x)}
	case int64:
		return map[string]any{"integerValue": fmt.Sprint(x)}
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e15 {
			return map[string]any{"integerValue": fmt.Sprint(int64(x))}
		}
		return map[string]any{"doubleValue": x}
	case time.Time:
		return map[string]any{"timestampValue": x.UTC().Format(time.RFC3339Nano)}
	case []any:
		vals := make([]any, 0, len(x))
		for _, e := range x {
			vals = append(vals, encodeValue(e))
		}
		return map[string]any{"arrayValue": map[string]any{"values": vals}}
	case map[string]any:
		f := map[string]any{}
		for k, e := range x {
			f[k] = encodeValue(e)
		}
		return map[string]any{"mapValue": map[string]any{"fields": f}}
	default:
		// structs/slices: normalise through JSON.
		b, err := json.Marshal(x)
		if err != nil {
			return map[string]any{"nullValue": nil}
		}
		var g any
		_ = json.Unmarshal(b, &g)
		return encodeValue(g)
	}
}
