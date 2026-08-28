package embedding

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
	"strings"
)

type OpenAIConfig struct {
	BaseURL    string
	APIKey     string
	Model      string
	Dimensions int
	BatchSize  int
	Client     *http.Client
}

type OpenAIEmbedder struct {
	baseURL    string
	apiKey     string
	model      string
	dimensions int
	batchSize  int
	client     *http.Client
}

const maxErrorResponseBytes = 64 << 10

// HTTPStatusError reports a non-successful response from an embedding API.
type HTTPStatusError struct {
	StatusCode int
	Status     string
	Body       string
}

func (e *HTTPStatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("embedding API returned %s", e.Status)
	}

	return fmt.Sprintf("embedding API returned %s: %s", e.Status, e.Body)
}

// NewOpenAIEmbedder creates an OpenAI-compatible embeddings client.
func NewOpenAIEmbedder(config OpenAIConfig) (*OpenAIEmbedder, error) {
	baseURL, err := embeddingEndpoint(config.BaseURL)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(config.APIKey) == "" {
		return nil, errors.New("embedding API key is required")
	}
	if strings.TrimSpace(config.Model) == "" {
		return nil, errors.New("embedding model is required")
	}
	if config.Dimensions <= 0 {
		return nil, errors.New("embedding dimensions must be greater than zero")
	}
	if config.BatchSize <= 0 {
		return nil, errors.New("embedding batch size must be greater than zero")
	}

	client := config.Client
	if client == nil {
		client = http.DefaultClient
	}

	return &OpenAIEmbedder{
		baseURL:    baseURL,
		apiKey:     strings.TrimSpace(config.APIKey),
		model:      strings.TrimSpace(config.Model),
		dimensions: config.Dimensions,
		batchSize:  config.BatchSize,
		client:     client,
	}, nil
}

func (e *OpenAIEmbedder) Dimensions() int {
	if e == nil {
		return 0
	}

	return e.dimensions
}

// Embed embeds texts in batches and preserves the input order in its response.
func (e *OpenAIEmbedder) Embed(
	ctx context.Context,
	texts []string,
) ([][]float32, error) {
	if e == nil {
		return nil, errors.New("OpenAI embedder is nil")
	}
	if e.client == nil {
		return nil, errors.New("embedding HTTP client is required")
	}
	if e.baseURL == "" || e.apiKey == "" || e.model == "" ||
		e.dimensions <= 0 || e.batchSize <= 0 {
		return nil, errors.New("OpenAI embedder is not configured")
	}
	if len(texts) == 0 {
		return nil, errors.New("embedding input is required")
	}

	embeddings := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += e.batchSize {
		end := min(start+e.batchSize, len(texts))
		batch, err := e.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, fmt.Errorf("embed batch %d-%d: %w", start, end, err)
		}
		embeddings = append(embeddings, batch...)
	}

	return embeddings, nil
}

type embeddingRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

func (e *OpenAIEmbedder) embedBatch(
	ctx context.Context,
	texts []string,
) ([][]float32, error) {
	body, err := json.Marshal(embeddingRequest{
		Model:      e.model,
		Input:      texts,
		Dimensions: e.dimensions,
	})
	if err != nil {
		return nil, fmt.Errorf("encode embedding request: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		e.baseURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+e.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := e.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request embedding API: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK ||
		response.StatusCode >= http.StatusMultipleChoices {
		data, readErr := io.ReadAll(io.LimitReader(
			response.Body,
			maxErrorResponseBytes,
		))
		if readErr != nil {
			return nil, fmt.Errorf("read embedding API error response: %w", readErr)
		}

		return nil, &HTTPStatusError{
			StatusCode: response.StatusCode,
			Status:     response.Status,
			Body:       strings.TrimSpace(string(data)),
		}
	}

	var decoded embeddingResponse
	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}

	if len(decoded.Data) != len(texts) {
		return nil, fmt.Errorf(
			"embedding response count %d does not match input count %d",
			len(decoded.Data),
			len(texts),
		)
	}

	embeddings := make([][]float32, len(texts))
	for _, item := range decoded.Data {
		if item.Index < 0 || item.Index >= len(embeddings) {
			return nil, fmt.Errorf("embedding response has invalid index %d", item.Index)
		}
		if embeddings[item.Index] != nil {
			return nil, fmt.Errorf("embedding response has duplicate index %d", item.Index)
		}
		if err := validateEmbedding(item.Embedding, e.dimensions); err != nil {
			return nil, fmt.Errorf(
				"embedding response item %d: %w",
				item.Index,
				err,
			)
		}

		embeddings[item.Index] = append([]float32(nil), item.Embedding...)
	}

	for index, item := range embeddings {
		if item == nil {
			return nil, fmt.Errorf("embedding response is missing index %d", index)
		}
	}

	return embeddings, nil
}

func embeddingEndpoint(base string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", errors.New("embedding base URL is required")
	}

	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse embedding base URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("embedding base URL must use http or https")
	}
	if parsed.Host == "" {
		return "", errors.New("embedding base URL must include a host")
	}
	if parsed.User != nil {
		return "", errors.New("embedding base URL must not include user credentials")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New(
			"embedding base URL must not include a query or fragment",
		)
	}

	for _, path := range []string{parsed.Path, parsed.EscapedPath()} {
		parts := strings.Split(strings.Trim(path, "/"), "/")
		for _, part := range parts {
			if part == "." || part == ".." {
				return "", errors.New(
					"embedding base URL must not contain relative path segments",
				)
			}
		}
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/embeddings"
	parsed.RawPath = ""
	return parsed.String(), nil
}

func validateEmbedding(values []float32, dimensions int) error {
	if len(values) != dimensions {
		return fmt.Errorf(
			"embedding dimensions %d do not match configured dimensions %d",
			len(values),
			dimensions,
		)
	}

	for _, value := range values {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return errors.New("embedding contains a non-finite value")
		}
	}

	return nil
}
