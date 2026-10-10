// Package rerank contains provider-neutral adapters for external rerank services.
package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
)

// Config describes the small HTTP contract expected by HTTPReranker.
//
// The endpoint receives:
//
//	{"model":"...","query":"...","documents":[{"id":"...","text":"..."}]}
//
// and returns:
//
//	{"results":[{"index":0,"relevance_score":0.91}]}
//
// Results must contain every input index exactly once. Keeping the adapter
// provider-neutral makes the ranking policy testable without embedding a
// vendor SDK in the API process.
type Config struct {
	Endpoint string
	APIKey   string
	Model    string
	Timeout  time.Duration
	Client   *http.Client
}

// HTTPReranker calls a configured external rerank endpoint.
type HTTPReranker struct {
	endpoint string
	apiKey   string
	model    string
	client   *http.Client
}

func NewHTTP(cfg Config) (*HTTPReranker, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		return nil, errors.New("rerank: endpoint is required")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("rerank: endpoint must be an http(s) URL")
	}
	client := cfg.Client
	if client == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = 2 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	return &HTTPReranker{endpoint: endpoint, apiKey: strings.TrimSpace(cfg.APIKey), model: strings.TrimSpace(cfg.Model), client: client}, nil
}

type request struct {
	Model     string          `json:"model,omitempty"`
	Query     string          `json:"query"`
	Documents []inputDocument `json:"documents"`
}

type inputDocument struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type response struct {
	Results []result `json:"results"`
}

type result struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

// Rerank implements rag.Reranker. The returned documents are the original
// pointers in descending relevance order; no document content is rewritten.
func (r *HTTPReranker) Rerank(ctx context.Context, query string, docs []*schema.Document) ([]*schema.Document, error) {
	if len(docs) == 0 {
		return []*schema.Document{}, nil
	}
	payload := request{Model: r.model, Query: query, Documents: make([]inputDocument, len(docs))}
	for i, doc := range docs {
		if doc == nil {
			return nil, fmt.Errorf("rerank: document %d is nil", i)
		}
		payload.Documents[i] = inputDocument{ID: doc.ID, Text: doc.Content}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("rerank: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("rerank: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if r.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+r.apiKey)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rerank: request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("rerank: endpoint returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	var decoded response
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("rerank: decode response: %w", err)
	}
	if len(decoded.Results) != len(docs) {
		return nil, fmt.Errorf("rerank: endpoint returned %d results, want %d", len(decoded.Results), len(docs))
	}
	seen := make(map[int]struct{}, len(docs))
	for _, item := range decoded.Results {
		if item.Index < 0 || item.Index >= len(docs) {
			return nil, fmt.Errorf("rerank: result index %d is out of range", item.Index)
		}
		if _, ok := seen[item.Index]; ok {
			return nil, fmt.Errorf("rerank: result index %d is duplicated", item.Index)
		}
		if math.IsNaN(item.RelevanceScore) || math.IsInf(item.RelevanceScore, 0) {
			return nil, fmt.Errorf("rerank: result index %d has non-finite score", item.Index)
		}
		seen[item.Index] = struct{}{}
	}
	sort.SliceStable(decoded.Results, func(i, j int) bool {
		if decoded.Results[i].RelevanceScore == decoded.Results[j].RelevanceScore {
			return decoded.Results[i].Index < decoded.Results[j].Index
		}
		return decoded.Results[i].RelevanceScore > decoded.Results[j].RelevanceScore
	})
	out := make([]*schema.Document, len(docs))
	for i, item := range decoded.Results {
		out[i] = docs[item.Index]
	}
	return out, nil
}
