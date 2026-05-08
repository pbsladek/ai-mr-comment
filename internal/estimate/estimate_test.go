package estimate

import (
	"context"
	"math"
	"testing"
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
		{"gpt-5.5", 1_000_000, 5.00, "Latest OpenAI exact match"},
		{"gpt-5.4-mini", 1_000_000, 0.75, "Latest OpenAI mini exact match"},
		{"gpt-4o-mini", 1_000_000, 0.15, "Legacy OpenAI exact match"},
		{"GPT-4o-Mini", 1_000_000, 0.15, "Case insensitive"},
		{"claude-3-5-sonnet-20240620", 1_000_000, 3.00, "Anthropic model"},
		{"gemini-1.5-flash", 1_000_000, 0.075, "Gemini Flash"},
		{"llama3", 1_000_000, 0.0, "Ollama/Llama (free)"},
		{"unknown-model", 1000, 0.0, "Unknown model"},
		{"custom-gpt-4o-mini-v2", 1_000_000, 0.15, "Fuzzy match"},
		{"custom-gpt-5.5-v2", 1_000_000, 5.00, "Fuzzy gpt-5.5"},
		{"custom-gpt-5.4-nano-v2", 1_000_000, 0.15, "Fuzzy gpt-5.4 nano"},
		{"custom-gpt-4o-v2", 1_000_000, 2.50, "Fuzzy gpt-4o"},
		{"claude-3-7-sonnet-custom", 1_000_000, 3.00, "Fuzzy claude-3-7"},
		{"gemini-2.0-flash-custom", 1_000_000, 0.10, "Fuzzy flash 2.0"},
		{"gemini-2.5-flash-custom", 1_000_000, 0.30, "Fuzzy flash 2.5"},
		{"custom-flash-model", 1_000_000, 0.0, "Non-gemini flash (unknown)"},
		{"gemini-pro-custom", 1_000_000, 1.25, "Fuzzy gemini pro"},
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

func TestEstimateCost_NonPositiveTokens(t *testing.T) {
	if got := EstimateCost("gpt-4o-mini", 0); got != 0 {
		t.Fatalf("expected 0 cost for zero tokens, got %f", got)
	}
	if got := EstimateCost("gpt-4o-mini", -1); got != 0 {
		t.Fatalf("expected 0 cost for negative tokens, got %f", got)
	}
	if got := EstimateCost("gpt-4o-mini", math.MinInt32); got != 0 {
		t.Fatalf("expected 0 cost for MinInt32 tokens, got %f", got)
	}
}
