package main

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/api/option"
)

func TestHeuristicTokenEstimator(t *testing.T) {
	e := &HeuristicTokenEstimator{}
	text := "Hello, world!" // 13 chars
	// 13 / 3.5 = 3.71 -> 4
	count, err := e.CountTokens(context.Background(), "any-model", text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 4 {
		t.Errorf("expected 4 tokens, got %d", count)
	}

	// Test with multiple strings
	count, err = e.CountTokens(context.Background(), "any-model", "Hello", ", ", "world!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 4 {
		t.Errorf("expected 4 tokens, got %d", count)
	}
}

func TestEstimateCost(t *testing.T) {
	tests := []struct {
		model       string
		tokens      int32
		expected    float64
		description string
	}{
		{"gpt-4o-mini", 1_000_000, 0.15, "Exact match"},
		{"GPT-4o-Mini", 1_000_000, 0.15, "Case insensitive"},
		{"claude-3-5-sonnet-20240620", 1_000_000, 3.00, "Anthropic model"},
		{"gemini-1.5-flash", 1_000_000, 0.075, "Gemini Flash"},
		{"llama3", 1_000_000, 0.0, "Ollama/Llama (free)"},
		{"unknown-model", 1000, 0.0, "Unknown model"},
		{"custom-gpt-4o-mini-v2", 1_000_000, 0.15, "Fuzzy match"},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			cost := EstimateCost(tc.model, tc.tokens)
			if math.Abs(cost-tc.expected) > 0.000001 {
				t.Errorf("expected cost %.6f for %s, got %.6f", tc.expected, tc.model, cost)
			}
		})
	}
}

func TestGeminiTokenEstimator_Mock(t *testing.T) {
	// Create a mock server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-1.5-flash:countTokens" {
			t.Errorf("Unexpected path: %s", r.URL.Path)
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}

		response := map[string]interface{}{
			"totalTokens": 10,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer ts.Close()

	// Inject the mock client options
	// Note: We use the package-level geminiClientOptions var in api.go/token_estimator.go
	geminiClientOptions = []option.ClientOption{
		option.WithEndpoint(ts.URL),
		option.WithHTTPClient(ts.Client()),
	}
	defer func() { geminiClientOptions = nil }()

	e := &GeminiTokenEstimator{APIKey: "test-key"}
	count, err := e.CountTokens(context.Background(), "gemini-1.5-flash", "test input")
	if err != nil {
		t.Fatalf("CountTokens failed: %v", err)
	}

	if count != 10 {
		t.Errorf("expected 10 tokens, got %d", count)
	}
}
