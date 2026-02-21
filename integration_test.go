//go:build integration

package main

import (
	"context"
	"encoding/json"
	"io"
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

func ollamaIntegrationEnv(t *testing.T) (endpoint, model string) {
	t.Helper()
	endpoint = os.Getenv("OLLAMA_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:11434/api/generate"
	}
	model = os.Getenv("OLLAMA_MODEL")
	if model == "" {
		model = "llama3.2:1b"
	}
	// loadConfig() reads this key via AI_MR_COMMENT_ prefix.
	t.Setenv("AI_MR_COMMENT_OLLAMA_ENDPOINT", endpoint)
	return endpoint, model
}

func isOllamaUnreachable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "couldn't connect")
}

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

func TestIntegration_Ollama(t *testing.T) {
	endpoint, model := ollamaIntegrationEnv(t)

	cfg := &Config{
		Provider:       Ollama,
		OllamaModel:    model,
		OllamaEndpoint: endpoint,
	}

	response, err := chatCompletions(context.Background(), cfg, Ollama, testSystemPrompt, testDiff)
	if err != nil {
		// Skip only when Ollama is unreachable.
		if isOllamaUnreachable(err) {
			t.Skipf("Skipping Ollama integration test: %v", err)
		}
		t.Fatalf("Ollama API call failed: %v", err)
	}

	if strings.TrimSpace(response) == "" {
		t.Error("Expected non-empty response from Ollama")
	}

	t.Logf("Ollama Response:\n%s", response)
}

func TestIntegration_Ollama_MainCmd_Text(t *testing.T) {
	_, model := ollamaIntegrationEnv(t)

	var out strings.Builder
	cmd := newRootCmd(chatCompletions)
	cmd.SetArgs([]string{
		"--provider=ollama",
		"--model=" + model,
		"--file=testdata/simple.diff",
	})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if isOllamaUnreachable(err) {
		t.Skipf("Skipping Ollama integration test: %v", err)
	}
	if err != nil {
		t.Fatalf("main command failed: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "── Description ──") {
		t.Fatalf("expected description section, got:\n%s", got)
	}
	if strings.TrimSpace(got) == "" {
		t.Fatal("expected non-empty output")
	}
}

func TestIntegration_Ollama_MainCmd_JSONTitle(t *testing.T) {
	_, model := ollamaIntegrationEnv(t)

	var out strings.Builder
	cmd := newRootCmd(chatCompletions)
	cmd.SetArgs([]string{
		"--provider=ollama",
		"--model=" + model,
		"--format=json",
		"--title",
		"--file=testdata/simple.diff",
	})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if isOllamaUnreachable(err) {
		t.Skipf("Skipping Ollama integration test: %v", err)
	}
	if err != nil {
		t.Fatalf("json+title command failed: %v", err)
	}

	var payload struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Provider    string `json:"provider"`
		Model       string `json:"model"`
	}
	if decErr := json.Unmarshal([]byte(out.String()), &payload); decErr != nil {
		t.Fatalf("invalid json output: %v\n%s", decErr, out.String())
	}
	if payload.Provider != string(Ollama) {
		t.Fatalf("expected provider=%q, got %q", Ollama, payload.Provider)
	}
	if payload.Model != model {
		t.Fatalf("expected model=%q, got %q", model, payload.Model)
	}
	if strings.TrimSpace(payload.Title) == "" {
		t.Fatal("expected non-empty title")
	}
	if strings.TrimSpace(payload.Description) == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestIntegration_Ollama_SmartChunk(t *testing.T) {
	_, model := ollamaIntegrationEnv(t)

	var out strings.Builder
	cmd := newRootCmd(chatCompletions)
	cmd.SetArgs([]string{
		"--provider=ollama",
		"--model=" + model,
		"--smart-chunk",
		"--file=testdata/multiple-files.diff",
	})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if isOllamaUnreachable(err) {
		t.Skipf("Skipping Ollama integration test: %v", err)
	}
	if err != nil {
		t.Fatalf("smart-chunk command failed: %v", err)
	}
	if strings.TrimSpace(out.String()) == "" {
		t.Fatal("expected non-empty smart-chunk output")
	}
}

func TestIntegration_Ollama_CommitMsg(t *testing.T) {
	_, model := ollamaIntegrationEnv(t)

	var out strings.Builder
	cmd := newRootCmd(chatCompletions)
	cmd.SetArgs([]string{
		"--provider=ollama",
		"--model=" + model,
		"--commit-msg",
		"--file=testdata/simple.diff",
	})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if isOllamaUnreachable(err) {
		t.Skipf("Skipping Ollama integration test: %v", err)
	}
	if err != nil {
		t.Fatalf("commit-msg command failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected a single-line commit message, got:\n%s", out.String())
	}
	if strings.TrimSpace(lines[0]) == "" {
		t.Fatal("expected non-empty commit message")
	}
}

func TestIntegration_Ollama_ChangelogJSON(t *testing.T) {
	_, model := ollamaIntegrationEnv(t)

	var out strings.Builder
	cmd := newRootCmd(chatCompletions)
	cmd.SetArgs([]string{
		"changelog",
		"--provider=ollama",
		"--model=" + model,
		"--format=json",
		"--file=testdata/multiple-files.diff",
	})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if isOllamaUnreachable(err) {
		t.Skipf("Skipping Ollama integration test: %v", err)
	}
	if err != nil {
		t.Fatalf("changelog command failed: %v", err)
	}

	var payload struct {
		Changelog string `json:"changelog"`
		Provider  string `json:"provider"`
		Model     string `json:"model"`
	}
	if decErr := json.Unmarshal([]byte(out.String()), &payload); decErr != nil {
		t.Fatalf("invalid changelog json: %v\n%s", decErr, out.String())
	}
	if payload.Provider != string(Ollama) {
		t.Fatalf("expected provider=%q, got %q", Ollama, payload.Provider)
	}
	if payload.Model != model {
		t.Fatalf("expected model=%q, got %q", model, payload.Model)
	}
	if strings.TrimSpace(payload.Changelog) == "" {
		t.Fatal("expected non-empty changelog")
	}
}

// TestIntegration_SmartChunk_Gemini verifies that --smart-chunk processes a
// large multi-file diff end-to-end using the real Gemini API: each file is
// summarised independently (in parallel), the summaries are synthesised into a
// final comment, and the output is non-empty.
func TestIntegration_SmartChunk_Gemini(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping smart-chunk integration test: GEMINI_API_KEY not set")
	}

	t.Setenv("GEMINI_API_KEY", apiKey)

	var buf strings.Builder
	cmd := newRootCmd(chatCompletions)
	cmd.SetArgs([]string{
		"--smart-chunk",
		"--file=testdata/large-multi-file.diff",
		"--provider=gemini",
		"--model=gemini-2.5-flash",
		"--verbose",
	})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "400") {
			t.Logf("Skipping: model unavailable (%v)", err)
			t.SkipNow()
		}
		t.Fatalf("smart-chunk command failed: %v", err)
	}

	out := buf.String()
	if strings.TrimSpace(out) == "" {
		t.Error("expected non-empty output from smart-chunk synthesis")
	}
	t.Logf("Smart-chunk synthesis output:\n%s", out)
}

// TestIntegration_SmartChunk_OpenAI verifies the same end-to-end behaviour
// against the real OpenAI API.
func TestIntegration_SmartChunk_OpenAI(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping smart-chunk integration test: OPENAI_API_KEY not set")
	}

	t.Setenv("OPENAI_API_KEY", apiKey)

	var buf strings.Builder
	cmd := newRootCmd(chatCompletions)
	cmd.SetArgs([]string{
		"--smart-chunk",
		"--file=testdata/large-multi-file.diff",
		"--provider=openai",
		"--model=gpt-4o-mini",
		"--verbose",
	})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "400") {
			t.Logf("Skipping: model unavailable (%v)", err)
			t.SkipNow()
		}
		t.Fatalf("smart-chunk command failed: %v", err)
	}

	out := buf.String()
	if strings.TrimSpace(out) == "" {
		t.Error("expected non-empty output from smart-chunk synthesis")
	}
	t.Logf("Smart-chunk synthesis output:\n%s", out)
}
