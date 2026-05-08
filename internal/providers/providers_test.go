package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pbsladek/ai-mr-comment/internal/config"
)

func TestCLIPromptStripsNullBytes(t *testing.T) {
	got := CLIPrompt("sys\x00tem", "di\x00ff")
	if strings.Contains(got, "\x00") {
		t.Fatalf("expected null bytes to be stripped, got %q", got)
	}
	if !strings.Contains(got, "system") || !strings.Contains(got, "diff") {
		t.Fatalf("expected content to be preserved, got %q", got)
	}
}

func TestCLIExecErrorContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := CLIExecError(ctx, "mybin", errors.New("exit status 1"), "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestValidateAPIKey(t *testing.T) {
	if err := ValidateAPIKey(config.OpenAI, &config.Config{}); err == nil {
		t.Fatal("expected missing OpenAI key error")
	}
	if err := ValidateAPIKey(config.ClaudeCLI, &config.Config{}); err != nil {
		t.Fatalf("expected CLI provider to skip API key validation, got %v", err)
	}
}

func TestGetOllamaHTTPTimeout(t *testing.T) {
	t.Setenv("AI_MR_COMMENT_OLLAMA_TIMEOUT_MS", "120000")
	if got := GetOllamaHTTPTimeout(); got != 2*time.Minute {
		t.Fatalf("expected 2m timeout, got %v", got)
	}

	t.Setenv("AI_MR_COMMENT_OLLAMA_TIMEOUT_MS", "bad")
	if got := GetOllamaHTTPTimeout(); got != DefaultOllamaHTTPTimeout {
		t.Fatalf("expected default timeout, got %v", got)
	}
}

func TestCallOllama(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"response": "ollama response"})
	}))
	defer ts.Close()

	cfg := &config.Config{
		OllamaModel:    "llama3",
		OllamaEndpoint: ts.URL,
	}

	got, err := CallOllama(context.Background(), cfg, "system", "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ollama response" {
		t.Fatalf("expected ollama response, got %q", got)
	}
}

func TestCLIArgs(t *testing.T) {
	cfg := &config.Config{
		ClaudeCLIModel: "claude-sonnet-4-6",
		GeminiCLIModel: "gemini-2.5-flash",
		CodexCLIModel:  "gpt-5.5",
	}

	if got := strings.Join(ClaudeCLIArgs(cfg, "prompt"), " "); !strings.Contains(got, "--model claude-sonnet-4-6") {
		t.Fatalf("expected Claude model arg, got %q", got)
	}
	if got := strings.Join(GeminiCLIArgs(cfg, "prompt"), " "); !strings.Contains(got, "--model gemini-2.5-flash") {
		t.Fatalf("expected Gemini model arg, got %q", got)
	}
	if got := strings.Join(CodexCLIArgs(cfg, "prompt"), " "); !strings.Contains(got, "-m gpt-5.5") {
		t.Fatalf("expected Codex model arg, got %q", got)
	}
}
