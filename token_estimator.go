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
	if inputTokens <= 0 {
		return 0.0
	}

	model = strings.ToLower(model)

	// Ollama models are local and free.
	if strings.Contains(model, "ollama") || strings.Contains(model, "llama") {
		return 0.0
	}

	// Pricing table in USD per 1M tokens (approximate, subject to change).
	prices := map[string]ModelPrice{
		// OpenAI (current)
		"o3":           {Input: 2.00},
		"o3-mini":      {Input: 1.10},
		"o3-pro":       {Input: 20.00},
		"o1":           {Input: 15.00},
		"o1-mini":      {Input: 1.10},
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
		"claude-opus-4-6":           {Input: 5.00},
		"claude-sonnet-4-6":         {Input: 3.00},
		"claude-haiku-4-5-20251001": {Input: 1.00},
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

	return substringFallbackPrice(model, inputTokens)
}

// substringRule matches a model name by one or two required substrings.
type substringRule struct {
	must1, must2 string
	inputPerM    float64
}

// substringFallbackPrice returns the cost for model name variants not in the
// exact pricing table, evaluated in priority order (most-specific first).
func substringFallbackPrice(model string, inputTokens int32) float64 {
	// Rules are checked in order; the first match wins.
	// More-specific patterns (e.g. "gpt-4.1-nano") must appear before
	// less-specific ones (e.g. "gpt-4.1") to avoid shadowing.
	rules := []substringRule{
		// OpenAI
		{"gpt-4.1-nano", "", 0.10},
		{"gpt-4.1-mini", "", 0.40},
		{"gpt-4.1", "", 2.00},
		{"gpt-4o-mini", "", 0.15},
		{"gpt-4o", "", 2.50},
		{"o3-mini", "", 1.10},
		{"o3", "", 2.00},
		// Anthropic
		{"claude", "opus", 5.00},
		{"claude", "sonnet", 3.00},
		{"claude", "haiku", 1.00},
		// Gemini — most-specific generation/tier first
		{"gemini-3", "pro", 2.00},
		{"gemini-3", "flash", 0.50},
		{"gemini-2.5", "pro", 1.25},
		{"gemini-2.5", "flash", 0.30},
		{"gemini", "flash", 0.10},
		{"gemini", "pro", 1.25},
	}

	for _, r := range rules {
		if strings.Contains(model, r.must1) && (r.must2 == "" || strings.Contains(model, r.must2)) {
			return float64(inputTokens) / 1_000_000 * r.inputPerM
		}
	}
	return 0.0
}
