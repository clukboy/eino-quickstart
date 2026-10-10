// Package llmsplit defines the provider-neutral HTTP contract for splitting a
// product catalogue that does not carry structured front matter.
package llmsplit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultMaxProducts = 100

// Product is one normalized product returned by the splitter.
type Product struct {
	ProductID string         `json:"product_id,omitempty"`
	Model     string         `json:"model,omitempty"`
	Name      string         `json:"product_name,omitempty"`
	Content   string         `json:"content"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// Config describes the small HTTP contract used by HTTPClient.
type Config struct {
	Endpoint      string
	APIKey        string
	Model         string
	ParserVersion string
	MaxProducts   int
	Timeout       time.Duration
	Client        *http.Client
}

// HTTPClient calls a model gateway. It deliberately does not depend on a
// vendor SDK, so parsing, validation and replay tests can run locally.
type HTTPClient struct {
	endpoint      string
	apiKey        string
	model         string
	parserVersion string
	maxProducts   int
	client        *http.Client
}

func NewHTTP(cfg Config) (*HTTPClient, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	u, err := url.Parse(endpoint)
	if endpoint == "" || err != nil || u.Scheme == "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("llmsplit: endpoint must be an http(s) URL")
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return nil, errors.New("llmsplit: model is required")
	}
	version := strings.TrimSpace(cfg.ParserVersion)
	if version == "" {
		return nil, errors.New("llmsplit: parser version is required")
	}
	maxProducts := cfg.MaxProducts
	if maxProducts <= 0 {
		maxProducts = DefaultMaxProducts
	}
	client := cfg.Client
	if client == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	return &HTTPClient{endpoint: endpoint, apiKey: strings.TrimSpace(cfg.APIKey), model: model, parserVersion: version, maxProducts: maxProducts, client: client}, nil
}

func (c *HTTPClient) Version() string { return c.parserVersion }

type request struct {
	Model         string `json:"model"`
	ParserVersion string `json:"parser_version"`
	Input         string `json:"input"`
}

type response struct {
	Products []Product `json:"products"`
}

// Split sends one input and rejects malformed or ambiguous model output before
// it can reach the database. API keys and input content are never included in
// returned error text.
func (c *HTTPClient) Split(ctx context.Context, input string) ([]Product, error) {
	if strings.TrimSpace(input) == "" {
		return nil, errors.New("llmsplit: input is required")
	}
	body, err := json.Marshal(request{Model: c.model, ParserVersion: c.parserVersion, Input: input})
	if err != nil {
		return nil, fmt.Errorf("llmsplit: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llmsplit: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llmsplit: request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("llmsplit: endpoint returned %s", resp.Status)
	}
	var decoded response
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("llmsplit: decode response: %w", err)
	}
	return Normalize(decoded.Products, c.maxProducts)
}

// Normalize validates and canonicalizes model output. A stable product key is
// mandatory: without one, concurrent retries cannot be made idempotent.
func Normalize(products []Product, maxProducts int) ([]Product, error) {
	if maxProducts <= 0 {
		maxProducts = DefaultMaxProducts
	}
	if len(products) == 0 {
		return nil, errors.New("llmsplit: endpoint returned no products")
	}
	if len(products) > maxProducts {
		return nil, fmt.Errorf("llmsplit: endpoint returned %d products, max is %d", len(products), maxProducts)
	}
	out := make([]Product, 0, len(products))
	seen := make(map[string]struct{}, len(products))
	for i, product := range products {
		product.ProductID = strings.TrimSpace(product.ProductID)
		product.Model = strings.TrimSpace(product.Model)
		product.Name = strings.TrimSpace(product.Name)
		product.Content = strings.TrimSpace(product.Content)
		if product.Content == "" {
			return nil, fmt.Errorf("llmsplit: product %d has empty content", i)
		}
		if product.Metadata == nil {
			product.Metadata = make(map[string]any)
		}
		if product.ProductID != "" {
			product.Metadata["product_id"] = product.ProductID
		}
		if product.Model != "" {
			product.Metadata["model"] = product.Model
		}
		if product.Name != "" {
			product.Metadata["product_name"] = product.Name
		}
		key := product.ProductID
		if key == "" {
			key, _ = product.Metadata["product_id"].(string)
		}
		if key == "" {
			key = product.Model
		}
		if key == "" {
			if value, ok := product.Metadata["model"].(string); ok {
				key = strings.TrimSpace(value)
			}
		}
		if key == "" {
			return nil, fmt.Errorf("llmsplit: product %d has no product_id or model", i)
		}
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("llmsplit: duplicate product key %q", key)
		}
		seen[key] = struct{}{}
		out = append(out, product)
	}
	return out, nil
}

// InputFingerprint lets callers persist the exact input/version boundary used
// for a replay without storing the full source in every product row.
func InputFingerprint(input, parserVersion, model string) string {
	sum := sha256.New()
	sum.Write([]byte(strings.TrimSpace(input)))
	sum.Write([]byte{0})
	sum.Write([]byte(strings.TrimSpace(parserVersion)))
	sum.Write([]byte{0})
	sum.Write([]byte(strings.TrimSpace(model)))
	return hex.EncodeToString(sum.Sum(nil))
}

// IsFinite is exported for adapters that add numeric confidence fields.
func IsFinite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
