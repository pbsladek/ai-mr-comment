package main

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// TokenEstimator defines the interface for counting tokens.
type TokenEstimator interface {
	CountTokens(ctx context.Context, modelName string, text ...string) (int32, error)
}

// GeminiTokenEstimator uses the official Go SDK to count tokens for Gemini models.
type GeminiTokenEstimator struct {
	APIKey string
}

func (e *GeminiTokenEstimator) CountTokens(ctx context.Context, modelName string, text ...string) (int32, error) {
	opts := []option.ClientOption{option.WithAPIKey(e.APIKey)}
	// Access geminiClientOptions from api.go if it exists in the same package (used for testing)
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

// HeuristicTokenEstimator provides a rough token estimate based on character count.
// This is used for providers (OpenAI, Anthropic, Ollama) whose SDKs do not
// currently expose a direct token counting function in a way that is easily accessible
// without extra dependencies or handling.
type HeuristicTokenEstimator struct{}

func (e *HeuristicTokenEstimator) CountTokens(_ context.Context, _ string, text ...string) (int32, error) {
	var totalChars int
	for _, t := range text {
		totalChars += len(t)
	}
	// A common heuristic is ~4 characters per token. We use a slightly more
	// conservative estimate of 3.5 to be safe.
	return int32(math.Ceil(float64(totalChars) / 3.5)), nil
}

// NewTokenEstimator returns the appropriate token estimator for the given provider.
func NewTokenEstimator(cfg *Config) TokenEstimator {
	switch cfg.Provider {
	case Gemini:
		return &GeminiTokenEstimator{APIKey: cfg.GeminiAPIKey}
	case OpenAI, Anthropic, Ollama:
		return &HeuristicTokenEstimator{}
	default:
		// Fallback to heuristic for any unknown provider.
		return &HeuristicTokenEstimator{}
	}
}

// ModelPrice represents the cost in USD per 1 Million tokens.
type ModelPrice struct {
	Input  float64
	Output float64
}

// EstimateCost returns the estimated cost in USD for the given number of input tokens.
// Prices are per 1 Million tokens.
func EstimateCost(model string, inputTokens int32) float64 {
	// Normalize model name
	model = strings.ToLower(model)

	// Ollama models are local and free
	if strings.Contains(model, "ollama") || strings.Contains(model, "llama") {
		return 0.0
	}

	// Pricing table (USD per 1M tokens)
	// Prices are approximate and subject to change.
	prices := map[string]ModelPrice{
		// OpenAI
		"gpt-4o":            {Input: 2.50, Output: 10.00},
		"gpt-4o-2024-05-13": {Input: 5.00, Output: 15.00},
		"gpt-4o-mini":       {Input: 0.15, Output: 0.60},
		"gpt-4-turbo":       {Input: 10.00, Output: 30.00},
		"gpt-3.5-turbo":     {Input: 0.50, Output: 1.50},

		// Anthropic
		"claude-3-5-sonnet-20240620": {Input: 3.00, Output: 15.00},
		"claude-3-7-sonnet-20250219": {Input: 3.00, Output: 15.00},
		"claude-3-opus-20240229":     {Input: 15.00, Output: 75.00},
		"claude-3-sonnet-20240229":   {Input: 3.00, Output: 15.00},
		"claude-3-haiku-20240307":    {Input: 0.25, Output: 1.25},

		// Gemini (Pricing for <= 128k context window)
		"gemini-1.5-pro":        {Input: 3.50, Output: 10.50},
		"gemini-1.5-pro-latest": {Input: 3.50, Output: 10.50},
		"gemini-1.5-flash":      {Input: 0.075, Output: 0.30},
		"gemini-2.0-flash":      {Input: 0.10, Output: 0.40},
		"gemini-2.5-flash":      {Input: 0.10, Output: 0.40},
	}

	// Direct lookup
	if price, ok := prices[model]; ok {
		return float64(inputTokens) / 1_000_000 * price.Input
	}

	// Fallback/heuristic matching if exact model name isn't found
	if strings.Contains(model, "gpt-4o-mini") {
		return float64(inputTokens) / 1_000_000 * 0.15
	}
	if strings.Contains(model, "gpt-4o") {
		return float64(inputTokens) / 1_000_000 * 2.50
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

	return 0.0 // Unknown model cost
}
