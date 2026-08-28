package embedding

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestOpenAIEmbedderBatchesAndOrdersResponses(t *testing.T) {
	var requests []embeddingRequest
	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/v1/embeddings" {
				t.Errorf("request path = %q, want /v1/embeddings", request.URL.Path)
			}
			if got := request.Header.Get("Authorization"); got != "Bearer test-key" {
				t.Errorf("authorization = %q, want bearer token", got)
			}

			var input embeddingRequest
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			requests = append(requests, input)

			data := make([]map[string]any, 0, len(input.Input))
			for index := len(input.Input) - 1; index >= 0; index-- {
				data = append(data, map[string]any{
					"index":     index,
					"embedding": []float32{float32(index + 1), float32(len(input.Input))},
				})
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"data": data})
		},
	))
	defer server.Close()

	embedder, err := NewOpenAIEmbedder(OpenAIConfig{
		BaseURL:    server.URL + "/v1/",
		APIKey:     "test-key",
		Model:      "text-embedding-test",
		Dimensions: 2,
		BatchSize:  2,
		Client:     server.Client(),
	})
	if err != nil {
		t.Fatalf("NewOpenAIEmbedder() error = %v", err)
	}

	got, err := embedder.Embed(context.Background(), []string{"one", "two", "three"})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}

	want := [][]float32{{1, 2}, {2, 2}, {1, 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Embed() = %v, want %v", got, want)
	}
	if len(requests) != 2 {
		t.Fatalf("request count = %d, want 2", len(requests))
	}
	if !reflect.DeepEqual(requests[0].Input, []string{"one", "two"}) ||
		!reflect.DeepEqual(requests[1].Input, []string{"three"}) {
		t.Errorf("batch inputs = %#v", requests)
	}
	for _, request := range requests {
		if request.Model != "text-embedding-test" || request.Dimensions != 2 {
			t.Errorf("unexpected request: %#v", request)
		}
	}
}

func TestOpenAIEmbedderReturnsStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = writer.Write([]byte(`{"error":{"message":"slow down"}}`))
		},
	))
	defer server.Close()

	embedder, err := NewOpenAIEmbedder(OpenAIConfig{
		BaseURL:    server.URL,
		APIKey:     "test-key",
		Model:      "text-embedding-test",
		Dimensions: 2,
		BatchSize:  1,
		Client:     server.Client(),
	})
	if err != nil {
		t.Fatalf("NewOpenAIEmbedder() error = %v", err)
	}

	_, err = embedder.Embed(context.Background(), []string{"one"})
	var statusError *HTTPStatusError
	if !errors.As(err, &statusError) {
		t.Fatalf("Embed() error = %v, want HTTPStatusError", err)
	}
	if statusError.StatusCode != http.StatusTooManyRequests ||
		statusError.Body != `{"error":{"message":"slow down"}}` {
		t.Errorf("status error = %#v", statusError)
	}
}

func TestNewOpenAIEmbedderValidatesConfiguration(t *testing.T) {
	valid := OpenAIConfig{
		BaseURL:    "https://example.test/v1",
		APIKey:     "key",
		Model:      "model",
		Dimensions: 2,
		BatchSize:  1,
	}

	for name, mutate := range map[string]func(*OpenAIConfig){
		"empty base URL": func(config *OpenAIConfig) {
			config.BaseURL = ""
		},
		"unsafe scheme": func(config *OpenAIConfig) {
			config.BaseURL = "file:///embedding"
		},
		"query string": func(config *OpenAIConfig) {
			config.BaseURL = "https://example.test/v1?token=value"
		},
		"relative path": func(config *OpenAIConfig) {
			config.BaseURL = "https://example.test/v1/../private"
		},
		"encoded relative path": func(config *OpenAIConfig) {
			config.BaseURL = "https://example.test/v1/%2e%2e/private"
		},
		"empty API key": func(config *OpenAIConfig) {
			config.APIKey = ""
		},
		"empty model": func(config *OpenAIConfig) {
			config.Model = ""
		},
		"zero dimensions": func(config *OpenAIConfig) {
			config.Dimensions = 0
		},
		"zero batch size": func(config *OpenAIConfig) {
			config.BatchSize = 0
		},
	} {
		t.Run(name, func(t *testing.T) {
			config := valid
			mutate(&config)

			if _, err := NewOpenAIEmbedder(config); err == nil {
				t.Error("NewOpenAIEmbedder() error = nil, want validation error")
			}
		})
	}
}
