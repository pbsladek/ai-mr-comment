package app

import (
	"context"
	"fmt"

	"github.com/google/generative-ai-go/genai"
	"github.com/pbsladek/ai-mr-comment/internal/estimate"
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
	// Reuse the shared client to avoid repeated TLS handshakes.
	client, err := getGeminiClient(ctx, e.APIKey)
	if err != nil {
		return 0, fmt.Errorf("failed to create genai client for token counting: %w", err)
	}

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
type HeuristicTokenEstimator = estimate.HeuristicTokenEstimator

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

// ModelPrice holds the input cost in USD per 1 million tokens.
type ModelPrice = estimate.ModelPrice

// EstimateCost returns the estimated USD cost for the given number of input tokens.
// It performs an exact lookup first, then falls back to substring matching.
// Returns 0 for unknown models or Ollama (which runs locally at no cost).
func EstimateCost(model string, inputTokens int32) float64 {
	return estimate.EstimateCost(model, inputTokens)
}
