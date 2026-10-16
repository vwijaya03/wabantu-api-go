package retrieval

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func geminiTestServer(t *testing.T, status int, dims int, capture *struct {
	key, path, body string
}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if capture != nil {
			capture.key = r.Header.Get("x-goog-api-key")
			capture.path = r.URL.String()
			capture.body = string(b)
		}
		var in struct {
			Requests []json.RawMessage `json:"requests"`
		}
		_ = json.Unmarshal(b, &in)
		if status != 200 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"status":"RESOURCE_EXHAUSTED","message":"echo of user text"}}`))
			return
		}
		type emb struct {
			Values []float32 `json:"values"`
		}
		out := struct {
			Embeddings []emb `json:"embeddings"`
		}{}
		for range in.Requests {
			v := make([]float32, dims)
			for i := range v {
				v[i] = 2 // not unit length: adapter must normalise
			}
			out.Embeddings = append(out.Embeddings, emb{Values: v})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
}

func TestGeminiEmbedderReturns1536NormalisedVectors(t *testing.T) {
	var cap struct{ key, path, body string }
	srv := geminiTestServer(t, 200, EmbeddingDims, &cap)
	defer srv.Close()
	e := NewGeminiEmbedder("test-key")
	e.baseURL = srv.URL

	vecs, err := e.Embed(context.Background(), []string{"halo, hubungi 081234567890", "dua"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 || len(vecs[0]) != EmbeddingDims {
		t.Fatalf("shape: %d vectors, dims %d", len(vecs), len(vecs[0]))
	}
	var sum float64
	for _, x := range vecs[0] {
		sum += float64(x) * float64(x)
	}
	if math.Abs(sum-1) > 1e-3 {
		t.Fatalf("vector not unit length: %f", sum)
	}
	if e.Model() != GeminiEmbeddingModel || e.Dimensions() != EmbeddingDims {
		t.Fatalf("model/dims: %s/%d", e.Model(), e.Dimensions())
	}
	if cap.key != "test-key" || strings.Contains(cap.path, "test-key") {
		t.Fatalf("api key must be sent in header only: key=%q url=%q", cap.key, cap.path)
	}
	if !strings.Contains(cap.path, "gemini-embedding-001:batchEmbedContents") {
		t.Fatalf("unexpected path %q", cap.path)
	}
	if !strings.Contains(cap.body, `"outputDimensionality":1536`) {
		t.Fatalf("missing outputDimensionality: %s", cap.body)
	}
	if strings.Contains(cap.body, "081234567890") {
		t.Fatal("PII must be redacted before the embedding API")
	}
}

func TestGeminiEmbedderRejectsWrongDims(t *testing.T) {
	srv := geminiTestServer(t, 200, 768, nil)
	defer srv.Close()
	e := NewGeminiEmbedder("k")
	e.baseURL = srv.URL
	if _, err := e.Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("expected dimension error")
	}
}

func TestGeminiEmbedderErrorOmitsBodyAndClassifies(t *testing.T) {
	srv := geminiTestServer(t, 429, 0, nil)
	defer srv.Close()
	e := NewGeminiEmbedder("k")
	e.baseURL = srv.URL
	_, err := e.Embed(context.Background(), []string{"secret user text"})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "echo of user text") || strings.Contains(err.Error(), "secret user text") {
		t.Fatalf("error must not carry body/input: %v", err)
	}
	if !IsRetryableError(err) {
		t.Fatalf("429 must be retryable: %v", err)
	}
}

func TestGeminiEmbedderBatchesAndTruncates(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, _ := io.ReadAll(r.Body)
		var in struct {
			Requests []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"requests"`
		}
		_ = json.Unmarshal(b, &in)
		if len(in.Requests) > geminiEmbedBatchMax {
			t.Errorf("batch too large: %d", len(in.Requests))
		}
		for _, rq := range in.Requests {
			if len(rq.Content.Parts[0].Text) > geminiEmbedMaxBytes {
				t.Errorf("text not truncated: %d bytes", len(rq.Content.Parts[0].Text))
			}
		}
		type emb struct {
			Values []float32 `json:"values"`
		}
		out := struct {
			Embeddings []emb `json:"embeddings"`
		}{}
		for range in.Requests {
			out.Embeddings = append(out.Embeddings, emb{Values: make([]float32, EmbeddingDims)})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	e := NewGeminiEmbedder("k")
	e.baseURL = srv.URL
	texts := make([]string, 205)
	for i := range texts {
		texts[i] = strings.Repeat("é", 10000)
	}
	vecs, err := e.Embed(context.Background(), texts)
	if err != nil || len(vecs) != 205 || calls != 3 {
		t.Fatalf("err=%v vecs=%d calls=%d", err, len(vecs), calls)
	}
}

func TestGeminiEmbedderRequiresKey(t *testing.T) {
	if _, err := NewGeminiEmbedder("  ").Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("expected not-configured error")
	}
}
