package main

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// TokenEstimator defines the interface for counting tokens before an API call.
type TokenEstimator interface {
	CountTokens(ctx context.Context, modelName string, text ...string) (int32, error)
}

// GeminiTokenEstimator uses the official Gemini SDK to count tokens exactly.
type GeminiTokenEstimator struct {
	APIKey string
}

// CountTokens returns the exact token count for the given texts using the Gemini
// countTokens API.
func (e *GeminiTokenEstimator) CountTokens(ctx context.Context, modelName string, text ...string) (int32, error) {
	opts := []option.ClientOption{option.WithAPIKey(e.APIKey)}
	// geminiClientOptions is declared in api.go and may be overridden in tests.
	opts = append(opts, geminiClientOptions...)

	client, err := genai.NewClient(ctx, opts...)
	if err != nil {
		return 0, fmt.Errorf("failed to create genai client for token counting: %w", err)
	}
	defer func() { _ = client.Close() }()

	model := client.GenerativeModel(modelName)
	parts := make([]genai.Part, len(text))
	for i, t := range text {
		parts[i] = genai.Text(t)
	}

	resp, err := model.CountTokens(ctx, parts...)
	if err != nil {
		return 0, err
	}
	return resp.TotalTokens, nil
}

// HeuristicTokenEstimator approximates token count from character length.
// Used for OpenAI, Anthropic, and Ollama which do not expose a free counting API.
type HeuristicTokenEstimator struct{}

// CountTokens estimates tokens using ~3.5 characters per token, which is
// slightly conservative compared to the common 4-char rule.
func (e *HeuristicTokenEstimator) CountTokens(_ context.Context, _ string, text ...string) (int32, error) {
	var totalChars int
	for _, t := range text {
		totalChars += len(t)
	}
	return int32(math.Ceil(float64(totalChars) / 3.5)), nil
}

// NewTokenEstimator returns the appropriate TokenEstimator for the configured provider.
func NewTokenEstimator(cfg *Config) TokenEstimator {
	switch cfg.Provider {
	case Gemini:
		return &GeminiTokenEstimator{APIKey: cfg.GeminiAPIKey}
	case OpenAI, Anthropic, Ollama:
		return &HeuristicTokenEstimator{}
	default:
		return &HeuristicTokenEstimator{}
	}
}

// ModelPrice holds the input and output cost in USD per 1 million tokens.
type ModelPrice struct {
	Input  float64
	Output float64
}

// EstimateCost returns the estimated USD cost for the given number of input tokens.
// It performs an exact lookup first, then falls back to substring matching.
// Returns 0 for unknown models or Ollama (which runs locally at no cost).
func EstimateCost(model string, inputTokens int32) float64 {
	model = strings.ToLower(model)

	// Ollama models are local and free.
	if strings.Contains(model, "ollama") || strings.Contains(model, "llama") {
		return 0.0
	}

	// Pricing table in USD per 1M tokens (approximate, subject to change).
	prices := map[string]ModelPrice{
		// OpenAI
		"gpt-4o":            {Input: 2.50, Output: 10.00},
		"gpt-4o-2024-05-13": {Input: 5.00, Output: 15.00},
		"gpt-4o-mini":       {Input: 0.15, Output: 0.60},
		"gpt-4-turbo":       {Input: 10.00, Output: 30.00},
		"gpt-3.5-turbo":     {Input: 0.50, Output: 1.50},

		// Anthropic (current)
		"claude-opus-4-6":            {Input: 5.00, Output: 25.00},
		"claude-sonnet-4-6":          {Input: 3.00, Output: 15.00},
		"claude-haiku-4-5-20251001":  {Input: 1.00, Output: 5.00},
		"claude-sonnet-4-5-20250929": {Input: 3.00, Output: 15.00},
		"claude-opus-4-5-20251101":   {Input: 5.00, Output: 25.00},
		"claude-sonnet-4-20250514":   {Input: 3.00, Output: 15.00},
		// Anthropic (legacy)
		"claude-3-7-sonnet-20250219": {Input: 3.00, Output: 15.00},
		"claude-3-5-sonnet-20240620": {Input: 3.00, Output: 15.00},
		"claude-3-opus-20240229":     {Input: 15.00, Output: 75.00},
		"claude-3-sonnet-20240229":   {Input: 3.00, Output: 15.00},
		"claude-3-haiku-20240307":    {Input: 0.25, Output: 1.25},

		// Gemini (pricing for ≤128k context window)
		"gemini-1.5-pro":        {Input: 3.50, Output: 10.50},
		"gemini-1.5-pro-latest": {Input: 3.50, Output: 10.50},
		"gemini-1.5-flash":      {Input: 0.075, Output: 0.30},
		"gemini-2.0-flash":      {Input: 0.10, Output: 0.40},
		"gemini-2.5-flash":      {Input: 0.10, Output: 0.40},
	}

	if price, ok := prices[model]; ok {
		return float64(inputTokens) / 1_000_000 * price.Input
	}

	// Substring fallback for model name variants not in the table.
	if strings.Contains(model, "gpt-4o-mini") {
		return float64(inputTokens) / 1_000_000 * 0.15
	}
	if strings.Contains(model, "gpt-4o") {
		return float64(inputTokens) / 1_000_000 * 2.50
	}
	// Claude 4.x sonnet / sonnet-4-5 / sonnet-4-6 aliases
	if strings.Contains(model, "claude-sonnet") || strings.Contains(model, "claude-opus-4") {
		if strings.Contains(model, "opus") {
			return float64(inputTokens) / 1_000_000 * 5.00
		}
		return float64(inputTokens) / 1_000_000 * 3.00
	}
	if strings.Contains(model, "claude-haiku") {
		return float64(inputTokens) / 1_000_000 * 1.00
	}
	if strings.Contains(model, "claude-3-5") || strings.Contains(model, "claude-3-7") {
		return float64(inputTokens) / 1_000_000 * 3.00
	}
	if strings.Contains(model, "flash") {
		if strings.Contains(model, "2.0") || strings.Contains(model, "2.5") {
			return float64(inputTokens) / 1_000_000 * 0.10
		}
		return float64(inputTokens) / 1_000_000 * 0.075
	}
	if strings.Contains(model, "pro") && strings.Contains(model, "gemini") {
		return float64(inputTokens) / 1_000_000 * 3.50
	}

	return 0.0
}
