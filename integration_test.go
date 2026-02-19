//go:build integration

package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

const testDiff = `diff --git a/test.txt b/test.txt
new file mode 100644
index 0000000..e69de29
--- /dev/null
+++ b/test.txt
@@ -0,0 +1 @@
+Hello World`

const testSystemPrompt = "You are a code reviewer. Summarize the changes. Be concise."

func TestIntegration_Gemini(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping Gemini integration test: GEMINI_API_KEY not set")
	}

	cfg := &Config{
		Provider:     Gemini,
		GeminiAPIKey: apiKey,
		GeminiModel:  "gemini-2.5-flash",
	}

	response, err := chatCompletions(context.Background(), cfg, Gemini, testSystemPrompt, testDiff)
	if err != nil {
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "400") {
			t.Logf("Skipping Gemini integration test: Model not found or unavailable (%v)", err)
			t.SkipNow()
		}
		t.Fatalf("Gemini API call failed: %v", err)
	}

	if strings.TrimSpace(response) == "" {
		t.Error("Expected non-empty response from Gemini")
	}

	t.Logf("Gemini Response:\n%s", response)
}

func TestIntegration_OpenAI(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping OpenAI integration test: OPENAI_API_KEY not set")
	}

	cfg := &Config{
		Provider:       OpenAI,
		OpenAIAPIKey:   apiKey,
		OpenAIModel:    "gpt-4o-mini",
		OpenAIEndpoint: "https://api.openai.com/v1/",
	}

	response, err := chatCompletions(context.Background(), cfg, OpenAI, testSystemPrompt, testDiff)
	if err != nil {
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "400") {
			t.Logf("Skipping OpenAI integration test: Model not found or unavailable (%v)", err)
			t.SkipNow()
		}
		t.Fatalf("OpenAI API call failed: %v", err)
	}

	if strings.TrimSpace(response) == "" {
		t.Error("Expected non-empty response from OpenAI")
	}

	t.Logf("OpenAI Response:\n%s", response)
}

func TestIntegration_Anthropic(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping Anthropic integration test: ANTHROPIC_API_KEY not set")
	}

	cfg := &Config{
		Provider:          Anthropic,
		AnthropicAPIKey:   apiKey,
		AnthropicModel:    "claude-sonnet-4-5",
		AnthropicEndpoint: "https://api.anthropic.com",
	}

	response, err := chatCompletions(context.Background(), cfg, Anthropic, testSystemPrompt, testDiff)
	if err != nil {
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "400") {
			t.Logf("Skipping Anthropic integration test: Model not found or unavailable (%v)", err)
			t.SkipNow()
		}
		t.Fatalf("Anthropic API call failed: %v", err)
	}

	if strings.TrimSpace(response) == "" {
		t.Error("Expected non-empty response from Anthropic")
	}

	t.Logf("Anthropic Response:\n%s", response)
}
