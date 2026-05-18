package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pbsladek/ai-mr-comment/internal/providers"
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

func TestNewTokenEstimator(t *testing.T) {
	tests := []struct {
		provider ApiProvider
		wantType string
	}{
		{Gemini, "*app.GeminiTokenEstimator"},
		{OpenAI, "*estimate.HeuristicTokenEstimator"},
		{Anthropic, "*estimate.HeuristicTokenEstimator"},
		{Ollama, "*estimate.HeuristicTokenEstimator"},
		{"unknown", "*estimate.HeuristicTokenEstimator"},
	}
	for _, tc := range tests {
		t.Run(string(tc.provider), func(t *testing.T) {
			cfg := &Config{Provider: tc.provider, GeminiAPIKey: "test"}
			est := NewTokenEstimator(cfg)
			got := fmt.Sprintf("%T", est)
			if got != tc.wantType {
				t.Errorf("expected %s, got %s", tc.wantType, got)
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
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer ts.Close()

	// Inject the mock client options
	// Note: We use the package-level geminiClientOptions var in api.go/token_estimator.go
	geminiClientOptions = []providers.GeminiClientOption{
		providers.GeminiEndpointOption(ts.URL, ts.Client()),
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
