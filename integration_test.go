package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

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

	// Use a simple prompt and diff for testing
	systemPrompt := "You are a code reviewer. Summarize the changes. Be concise."
	diffContent := `diff --git a/test.txt b/test.txt
new file mode 100644
index 0000000..e69de29
--- /dev/null
+++ b/test.txt
@@ -0,0 +1 @@
+Hello World`

	response, err := chatCompletions(context.Background(), cfg, Gemini, systemPrompt, diffContent)
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
