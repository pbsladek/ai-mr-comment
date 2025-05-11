package main

import (
	"strings"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	text := "This is a short sentence."
	tokens := estimateTokens(text)
	if tokens <= 0 {
		t.Errorf("Expected positive token count, got %d", tokens)
	}
}

func TestEstimateDebugOutput(t *testing.T) {
	text := strings.Repeat("x", 3500) // ~1000 tokens
	tokens := estimateTokens(text)
	if tokens < 900 || tokens > 1100 {
		t.Errorf("Unexpected token estimate: %d", tokens)
	}
}
