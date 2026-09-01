package retrieval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// VectorStore abstracts Pinecone (or mock) vector operations.
type VectorStore interface {
	Upsert(ctx context.Context, namespace string, records []VectorRecord) error
	Query(ctx context.Context, namespace string, vector []float32, topK int, filter map[string]any) ([]Hit, error)
	DeleteIDs(ctx context.Context, namespace string, ids []string) error
	DeleteByFilter(ctx context.Context, namespace string, filter map[string]any) error
	DeleteNamespace(ctx context.Context, namespace string) error
}

// PineconeClient talks to Pinecone index host via REST.
type PineconeClient struct {
	host       string
	apiKey     string
	httpClient *http.Client
}

func NewPineconeClient(host, apiKey string) *PineconeClient {
	host = normalizePineconeHost(host)
	return &PineconeClient{
		host:   host,
		apiKey: strings.TrimSpace(apiKey),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func normalizePineconeHost(host string) string {
	host = strings.TrimSpace(host)
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	return strings.TrimSuffix(host, "/")
}

func (c *PineconeClient) Upsert(ctx context.Context, namespace string, records []VectorRecord) error {
	if c == nil || c.host == "" || c.apiKey == "" {
		return fmt.Errorf("pinecone not configured")
	}
	if len(records) == 0 {
		return nil
	}
	body := pineconeUpsertRequest{Vectors: make([]pineconeVector, len(records)), Namespace: namespace}
	for i, r := range records {
		body.Vectors[i] = pineconeVector{ID: r.ID, Values: r.Values, Metadata: r.Metadata}
	}
	return c.post(ctx, "/vectors/upsert", body, nil)
}

func (c *PineconeClient) Query(ctx context.Context, namespace string, vector []float32, topK int, filter map[string]any) ([]Hit, error) {
	if c == nil || c.host == "" || c.apiKey == "" {
		return nil, fmt.Errorf("pinecone not configured")
	}
	if topK <= 0 {
		topK = 8
	}
	req := pineconeQueryRequest{
		Vector:          vector,
		TopK:            topK,
		Namespace:       namespace,
		IncludeMetadata: true,
		Filter:          filter,
	}
	var resp pineconeQueryResponse
	if err := c.post(ctx, "/query", req, &resp); err != nil {
		return nil, err
	}
	hits := make([]Hit, len(resp.Matches))
	for i, m := range resp.Matches {
		hits[i] = Hit{ID: m.ID, Score: m.Score, Metadata: m.Metadata}
	}
	return hits, nil
}

func (c *PineconeClient) DeleteIDs(ctx context.Context, namespace string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return c.post(ctx, "/vectors/delete", pineconeDeleteRequest{IDs: ids, Namespace: namespace}, nil)
}

func (c *PineconeClient) DeleteByFilter(ctx context.Context, namespace string, filter map[string]any) error {
	if filter == nil {
		return nil
	}
	return c.post(ctx, "/vectors/delete", pineconeDeleteRequest{Filter: filter, Namespace: namespace}, nil)
}

func (c *PineconeClient) DeleteNamespace(ctx context.Context, namespace string) error {
	if namespace == "" {
		return nil
	}
	return c.post(ctx, "/vectors/delete", pineconeDeleteRequest{DeleteAll: true, Namespace: namespace}, nil)
}

func (c *PineconeClient) post(ctx context.Context, path string, body any, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	url := "https://" + c.host + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Api-Key", c.apiKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("pinecone %s: status %d: %s", path, resp.StatusCode, truncateErr(string(raw), 500))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return err
		}
	}
	return nil
}

func truncateErr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

type pineconeUpsertRequest struct {
	Vectors   []pineconeVector `json:"vectors"`
	Namespace string           `json:"namespace,omitempty"`
}

type pineconeVector struct {
	ID       string         `json:"id"`
	Values   []float32      `json:"values"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type pineconeQueryRequest struct {
	Vector          []float32      `json:"vector"`
	TopK            int            `json:"topK"`
	Namespace       string         `json:"namespace,omitempty"`
	IncludeMetadata bool           `json:"includeMetadata"`
	Filter          map[string]any `json:"filter,omitempty"`
}

type pineconeQueryResponse struct {
	Matches []struct {
		ID       string         `json:"id"`
		Score    float64        `json:"score"`
		Metadata map[string]any `json:"metadata"`
	} `json:"matches"`
}

type pineconeDeleteRequest struct {
	IDs       []string       `json:"ids,omitempty"`
	Filter    map[string]any `json:"filter,omitempty"`
	Namespace string         `json:"namespace,omitempty"`
	DeleteAll bool           `json:"deleteAll,omitempty"`
}

// PineconeConfigured returns true when host and key are set.
func PineconeConfigured(host, apiKey string) bool {
	return strings.TrimSpace(host) != "" && strings.TrimSpace(apiKey) != ""
}
