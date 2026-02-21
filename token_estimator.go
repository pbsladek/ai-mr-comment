package main

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/google/generative-ai-go/genai"
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

// ModelPrice holds the input cost in USD per 1 million tokens.
type ModelPrice struct {
	Input float64
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
		// OpenAI (current)
		"o3":        {Input: 2.00},
		"o3-mini":   {Input: 1.10},
		"o3-pro":    {Input: 20.00},
		"o1":        {Input: 15.00},
		"o1-mini":   {Input: 1.10},
		"gpt-4.1":      {Input: 2.00},
		"gpt-4.1-mini": {Input: 0.40},
		"gpt-4.1-nano": {Input: 0.10},
		// OpenAI (legacy)
		"gpt-4o":            {Input: 2.50},
		"gpt-4o-2024-05-13": {Input: 5.00},
		"gpt-4o-mini":       {Input: 0.15},
		"gpt-4-turbo":       {Input: 10.00},
		"gpt-3.5-turbo":     {Input: 0.50},

		// Anthropic (current)
		"claude-opus-4-6":            {Input: 5.00},
		"claude-sonnet-4-6":          {Input: 3.00},
		"claude-haiku-4-5-20251001":  {Input: 1.00},
		// Anthropic (legacy)
		"claude-opus-4-5-20251101":   {Input: 5.00},
		"claude-opus-4-1-20250805":   {Input: 15.00},
		"claude-opus-4-20250514":     {Input: 15.00},
		"claude-sonnet-4-5-20250929": {Input: 3.00},
		"claude-sonnet-4-20250514":   {Input: 3.00},
		"claude-3-7-sonnet-20250219": {Input: 3.00},
		"claude-3-5-sonnet-20240620": {Input: 3.00},
		"claude-3-opus-20240229":     {Input: 15.00},
		"claude-3-sonnet-20240229":   {Input: 3.00},
		"claude-3-haiku-20240307":    {Input: 0.25},

		// Gemini (current; pricing for text ≤200k context window)
		"gemini-2.5-pro":        {Input: 1.25},
		"gemini-2.5-flash":      {Input: 0.30},
		"gemini-2.5-flash-lite": {Input: 0.10},
		// Gemini (preview)
		"gemini-3-flash-preview": {Input: 0.50},
		"gemini-3-pro-preview":   {Input: 2.00},
		// Gemini (legacy — retiring June 2026)
		"gemini-2.0-flash":      {Input: 0.10},
		"gemini-2.0-flash-lite": {Input: 0.075},
		"gemini-1.5-pro":        {Input: 3.50},
		"gemini-1.5-pro-latest": {Input: 3.50},
		"gemini-1.5-flash":      {Input: 0.075},
	}

	if price, ok := prices[model]; ok {
		return float64(inputTokens) / 1_000_000 * price.Input
	}

	// Substring fallback for model name variants not in the exact table.
	// OpenAI
	if strings.Contains(model, "gpt-4.1-nano") {
		return float64(inputTokens) / 1_000_000 * 0.10
	}
	if strings.Contains(model, "gpt-4.1-mini") {
		return float64(inputTokens) / 1_000_000 * 0.40
	}
	if strings.Contains(model, "gpt-4.1") {
		return float64(inputTokens) / 1_000_000 * 2.00
	}
	if strings.Contains(model, "gpt-4o-mini") {
		return float64(inputTokens) / 1_000_000 * 0.15
	}
	if strings.Contains(model, "gpt-4o") {
		return float64(inputTokens) / 1_000_000 * 2.50
	}
	if strings.Contains(model, "o3-mini") {
		return float64(inputTokens) / 1_000_000 * 1.10
	}
	if strings.Contains(model, "o3") {
		return float64(inputTokens) / 1_000_000 * 2.00
	}
	// Anthropic
	if strings.Contains(model, "claude") && strings.Contains(model, "opus") {
		return float64(inputTokens) / 1_000_000 * 5.00
	}
	if strings.Contains(model, "claude") && strings.Contains(model, "sonnet") {
		return float64(inputTokens) / 1_000_000 * 3.00
	}
	if strings.Contains(model, "claude") && strings.Contains(model, "haiku") {
		return float64(inputTokens) / 1_000_000 * 1.00
	}
	// Gemini
	if strings.Contains(model, "gemini-3") && strings.Contains(model, "pro") {
		return float64(inputTokens) / 1_000_000 * 2.00
	}
	if strings.Contains(model, "gemini-3") && strings.Contains(model, "flash") {
		return float64(inputTokens) / 1_000_000 * 0.50
	}
	if strings.Contains(model, "gemini-2.5") && strings.Contains(model, "pro") {
		return float64(inputTokens) / 1_000_000 * 1.25
	}
	if strings.Contains(model, "gemini-2.5") && strings.Contains(model, "flash") {
		return float64(inputTokens) / 1_000_000 * 0.30
	}
	if strings.Contains(model, "gemini") && strings.Contains(model, "flash") {
		return float64(inputTokens) / 1_000_000 * 0.10
	}
	if strings.Contains(model, "gemini") && strings.Contains(model, "pro") {
		return float64(inputTokens) / 1_000_000 * 1.25
	}

	return 0.0
}
