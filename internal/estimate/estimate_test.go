package estimate

import (
	"context"
	"math"
	"testing"
	"time"
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
		{"gpt-5.4-nano", 1_000_000, 0.20, "Latest OpenAI nano exact match"},
		{"gpt-5.3-codex", 1_000_000, 1.75, "Codex OpenAI exact match"},
		{"gpt-4o-mini", 1_000_000, 0.15, "Legacy OpenAI exact match"},
		{"GPT-4o-Mini", 1_000_000, 0.15, "Case insensitive"},
		{"claude-opus-4-7", 1_000_000, 5.00, "Latest Anthropic model"},
		{"claude-3-5-sonnet-20240620", 1_000_000, 3.00, "Anthropic model"},
		{"gemini-3.1-pro-preview", 1_000_000, 2.00, "Latest Gemini Pro"},
		{"gemini-3.1-flash-lite", 1_000_000, 0.25, "Latest Gemini Flash-Lite"},
		{"gemini-1.5-flash", 1_000_000, 0.075, "Gemini Flash"},
		{"llama3", 1_000_000, 0.0, "Ollama/Llama (free)"},
		{"unknown-model", 1000, 0.0, "Unknown model"},
		{"custom-gpt-4o-mini-v2", 1_000_000, 0.15, "Fuzzy match"},
		{"custom-gpt-5.5-v2", 1_000_000, 5.00, "Fuzzy gpt-5.5"},
		{"custom-gpt-5.4-nano-v2", 1_000_000, 0.20, "Fuzzy gpt-5.4 nano"},
		{"custom-gpt-4o-v2", 1_000_000, 2.50, "Fuzzy gpt-4o"},
		{"claude-3-7-sonnet-custom", 1_000_000, 3.00, "Fuzzy claude-3-7"},
		{"gemini-2.0-flash-custom", 1_000_000, 0.10, "Fuzzy flash 2.0"},
		{"gemini-2.5-flash-custom", 1_000_000, 0.30, "Fuzzy flash 2.5"},
		{"gemini-2.5-flash-lite-preview-custom", 1_000_000, 0.10, "Fuzzy flash-lite 2.5"},
		{"gemini-3.1-flash-lite-custom", 1_000_000, 0.25, "Fuzzy flash-lite 3.1"},
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

func TestCurrentModelPricingAndKnownStatus(t *testing.T) {
	now := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		model  string
		tokens int32
		want   float64
	}{
		{"gpt-6-astra", 1000000, 10}, {"gpt-6.1-sol", 1000000, 2}, {"gpt-6.1-terra", 1000000, 1}, {"gpt-6-luna", 1000000, 0.10},
		{"claude-fable-5-1", 1000000, 10}, {"claude-opus-5-5", 1000000, 4}, {"claude-sonnet-5-5", 1000000, 2},
		{"claude-haiku-5-5", 100000, 0.01}, {"claude-haiku-5-5", 100001, 0.0500005},
		{"gemini-4-argon", 1000000, 2}, {"gemini-3.8-flash", 1000000, 0.75}, {"gemini-3.5-flash-lite", 1000000, 0.30},
	} {
		got, known := lookupCostAt(tc.model, tc.tokens, now)
		if !known || math.Abs(got-tc.want) > 0.0000001 {
			t.Errorf("%s(%d) = %f, %v; want %f", tc.model, tc.tokens, got, known, tc.want)
		}
	}
	for _, model := range []string{"future-model", "hosted-llama", "claude-future-sonnet", "gemini-future-flash", "custom-gpt-6.1-sol"} {
		if got, known := lookupCostAt(model, 1000, now); known || got != 0 {
			t.Errorf("unknown model %q received price %f, %v", model, got, known)
		}
	}
	for _, tc := range []struct {
		model string
		date  time.Time
		want  float64
	}{
		{"gemini-4-argon", time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), 4},
		{"gemini-3.8-flash", time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), 1.50},
	} {
		if got, known := lookupCostAt(tc.model, 1000000, tc.date); !known || got != tc.want {
			t.Errorf("promotional price end for %s: %f, %v", tc.model, got, known)
		}
	}
}
