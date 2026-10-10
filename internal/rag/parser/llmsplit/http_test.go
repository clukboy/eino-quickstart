package llmsplit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPClientSplitValidatesAndSendsContract(t *testing.T) {
	var got request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("authorization header missing")
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(response{Products: []Product{{ProductID: "P-1", Name: "门铰链", Content: "规格正文"}}})
	}))
	defer srv.Close()

	client, err := NewHTTP(Config{Endpoint: srv.URL, APIKey: "secret", Model: "split-v1", ParserVersion: "p16-v1", Client: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	products, err := client.Split(context.Background(), "没有 front-matter 的原文")
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "split-v1" || got.ParserVersion != "p16-v1" || got.Input == "" {
		t.Fatalf("request contract incorrect: %#v", got)
	}
	if len(products) != 1 || products[0].Metadata["product_id"] != "P-1" {
		t.Fatalf("normalized product incorrect: %#v", products)
	}
}

func TestNormalizeRejectsAmbiguousProducts(t *testing.T) {
	cases := []struct {
		name string
		in   []Product
		want string
	}{
		{"empty", nil, "no products"},
		{"no-key", []Product{{Content: "body"}}, "no product_id or model"},
		{"duplicate", []Product{{ProductID: "P-1", Content: "a"}, {Model: "P-1", Content: "b"}}, "duplicate"},
		{"empty-content", []Product{{ProductID: "P-1"}}, "empty content"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Normalize(tc.in, 10)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestInputFingerprintIncludesParserVersionAndModel(t *testing.T) {
	base := InputFingerprint("source", "v1", "model")
	if base == InputFingerprint("source", "v2", "model") || base == InputFingerprint("source", "v1", "other") {
		t.Fatal("parser version and model must participate in replay fingerprint")
	}
	if base != InputFingerprint(" source ", "v1", "model") {
		t.Fatal("whitespace normalization should be stable")
	}
}
