package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	openai "github.com/openai/openai-go"
	openaiopt "github.com/openai/openai-go/option"
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

func TestGeminiGenerateConfigDoesNotCapOutputTokens(t *testing.T) {
	cfg := geminiGenerateConfig("system")
	if cfg.MaxOutputTokens != 0 {
		t.Fatalf("expected Gemini max output tokens to be unset, got %d", cfg.MaxOutputTokens)
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

func TestCLIExecErrorUsesStderrAndWrappedError(t *testing.T) {
	err := CLIExecError(context.Background(), "mybin", errors.New("exit status 1"), " bad things happened \n")
	if !strings.Contains(err.Error(), "mybin: bad things happened") {
		t.Fatalf("expected stderr in error, got %v", err)
	}

	wrapped := errors.New("exit status 2")
	err = CLIExecError(context.Background(), "mybin", wrapped, "")
	if !errors.Is(err, wrapped) {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func TestProviderErrorEnrichment(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://api.example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp := &http.Response{StatusCode: http.StatusUnauthorized, Status: "401 Unauthorized", Request: req}

	openAIErr := enrichOpenAIError(&openai.Error{StatusCode: http.StatusUnauthorized, Request: req, Response: resp})
	if !strings.Contains(openAIErr.Error(), "OpenAI API key is invalid") {
		t.Fatalf("expected OpenAI auth guidance, got %v", openAIErr)
	}
	anthropicErr := enrichAnthropicError(&anthropic.Error{StatusCode: http.StatusTooManyRequests, Request: req, Response: &http.Response{StatusCode: http.StatusTooManyRequests, Status: "429 Too Many Requests", Request: req}})
	if !strings.Contains(anthropicErr.Error(), "Anthropic rate limit") {
		t.Fatalf("expected Anthropic rate-limit guidance, got %v", anthropicErr)
	}
	dnsErr := enrichNetworkError(&net.DNSError{Name: "api.example.test", IsNotFound: true})
	if !strings.Contains(dnsErr.Error(), "Could not reach the API host") {
		t.Fatalf("expected DNS guidance, got %v", dnsErr)
	}
}

func TestValidateAPIKey(t *testing.T) {
	if err := ValidateAPIKey(config.OpenAI, &config.Config{}); err == nil {
		t.Fatal("expected missing OpenAI key error")
	}
	if err := ValidateAPIKey(config.Anthropic, &config.Config{}); err == nil {
		t.Fatal("expected missing Anthropic key error")
	}
	if err := ValidateAPIKey(config.Gemini, &config.Config{}); err == nil {
		t.Fatal("expected missing Gemini key error")
	}
	if err := ValidateAPIKey(config.OpenAI, &config.Config{OpenAIAPIKey: "key"}); err != nil {
		t.Fatalf("expected OpenAI key to pass, got %v", err)
	}
	if err := ValidateAPIKey(config.ClaudeCLI, &config.Config{}); err != nil {
		t.Fatalf("expected CLI provider to skip API key validation, got %v", err)
	}
}

func TestSetGeminiClientOptionsClearsCacheState(t *testing.T) {
	SetGeminiClientOptions(nil)
	SetGeminiClientOptions(nil)
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

func TestCallOllamaAPIErrorIncludesJSONBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "model unavailable"})
	}))
	defer ts.Close()

	_, err := CallOllama(context.Background(), &config.Config{OllamaModel: "llama3", OllamaEndpoint: ts.URL}, "system", "diff")
	if err == nil || !strings.Contains(err.Error(), "model unavailable") {
		t.Fatalf("expected ollama API error body, got %v", err)
	}
}

func TestCallOpenAIEnrichesAPIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key","type":"invalid_request_error","code":"invalid_api_key"}}`))
	}))
	defer ts.Close()

	client := openai.NewClient(openaiopt.WithAPIKey("bad"), openaiopt.WithBaseURL(ts.URL))
	_, err := CallOpenAI(context.Background(), &client, &config.Config{OpenAIModel: "gpt-5.5"}, "system", "diff")
	if err == nil || !strings.Contains(err.Error(), "OpenAI API key is invalid") {
		t.Fatalf("expected enriched OpenAI auth error, got %v", err)
	}
}

func TestCallAnthropicEnrichesAPIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	}))
	defer ts.Close()

	client := anthropic.NewClient(anthropicopt.WithAPIKey("bad"), anthropicopt.WithBaseURL(ts.URL+"/"))
	_, err := CallAnthropic(context.Background(), &client, &config.Config{AnthropicModel: "claude-sonnet-4-6"}, "system", "diff")
	if err == nil || !strings.Contains(err.Error(), "Anthropic rate limit") {
		t.Fatalf("expected enriched Anthropic rate-limit error, got %v", err)
	}
}

func TestStreamOpenAIEnrichesAPIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"forbidden","type":"invalid_request_error","code":"forbidden"}}`))
	}))
	defer ts.Close()

	client := openai.NewClient(openaiopt.WithAPIKey("bad"), openaiopt.WithBaseURL(ts.URL))
	_, err := StreamOpenAI(context.Background(), &client, &config.Config{OpenAIModel: "gpt-5.5"}, "system", "diff", ioDiscard{})
	if err == nil || !strings.Contains(err.Error(), "OpenAI API key is invalid") {
		t.Fatalf("expected enriched OpenAI stream auth error, got %v", err)
	}
}

func TestStreamAnthropicEnrichesAPIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"type":"overloaded_error","message":"try later"}}`))
	}))
	defer ts.Close()

	client := anthropic.NewClient(anthropicopt.WithAPIKey("bad"), anthropicopt.WithBaseURL(ts.URL+"/"))
	_, err := StreamAnthropic(context.Background(), &client, &config.Config{AnthropicModel: "claude-sonnet-4-6"}, "system", "diff", ioDiscard{})
	if err == nil || !strings.Contains(err.Error(), "Anthropic API returned a server error") {
		t.Fatalf("expected enriched Anthropic stream server error, got %v", err)
	}
}

func TestStreamOllama(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = fmt.Fprintln(w, `{"response":"hello ","done":false}`)
		_, _ = fmt.Fprintln(w, `{"response":"world","done":true}`)
	}))
	defer ts.Close()

	var buf bytes.Buffer
	got, err := StreamOllama(context.Background(), &config.Config{OllamaModel: "llama3", OllamaEndpoint: ts.URL}, "system", "diff", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello world" || buf.String() != got {
		t.Fatalf("stream result mismatch: got=%q writer=%q", got, buf.String())
	}
}

func TestStreamOllamaReturnsWriterError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = fmt.Fprintln(w, `{"response":"hello","done":true}`)
	}))
	defer ts.Close()

	_, err := StreamOllama(context.Background(), &config.Config{OllamaModel: "llama3", OllamaEndpoint: ts.URL}, "system", "diff", errWriter{err: errors.New("closed pipe")})
	if err == nil || !strings.Contains(err.Error(), "closed pipe") {
		t.Fatalf("expected writer error, got %v", err)
	}
}

func TestStreamOllamaMalformedChunk(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = fmt.Fprintln(w, `{bad json`)
	}))
	defer ts.Close()

	_, err := StreamOllama(context.Background(), &config.Config{OllamaModel: "llama3", OllamaEndpoint: ts.URL}, "system", "diff", ioDiscard{})
	if err == nil || !strings.Contains(err.Error(), "decoding ollama stream chunk") {
		t.Fatalf("expected malformed chunk error, got %v", err)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

type errWriter struct {
	err error
}

func (w errWriter) Write([]byte) (int, error) { return 0, w.err }

func writeExecutable(t *testing.T, dir, name, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".bat"
	}
	path := filepath.Join(dir, name)
	if runtime.GOOS == "windows" {
		body = "@echo off\r\n" + body
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExecCLIAndStreamExecCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixtures are POSIX-only")
	}
	dir := t.TempDir()
	bin := writeExecutable(t, dir, "ai-cli", "#!/bin/sh\necho cli output\n")

	got, err := ExecCLI(context.Background(), bin, nil)
	if err != nil {
		t.Fatalf("ExecCLI returned error: %v", err)
	}
	if got != "cli output" {
		t.Fatalf("ExecCLI = %q", got)
	}

	var buf bytes.Buffer
	got, err = StreamExecCLI(context.Background(), bin, nil, &buf)
	if err != nil {
		t.Fatalf("StreamExecCLI returned error: %v", err)
	}
	if got != "cli output" || strings.TrimSpace(buf.String()) != got {
		t.Fatalf("stream mismatch: got=%q writer=%q", got, buf.String())
	}
}

func TestExecCLIErrorAndEmptyOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixtures are POSIX-only")
	}
	dir := t.TempDir()
	fail := writeExecutable(t, dir, "fail-cli", "#!/bin/sh\necho nope >&2\nexit 7\n")
	empty := writeExecutable(t, dir, "empty-cli", "#!/bin/sh\nexit 0\n")

	if _, err := ExecCLI(context.Background(), fail, nil); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("expected stderr failure, got %v", err)
	}
	if _, err := ExecCLI(context.Background(), empty, nil); err == nil || !strings.Contains(err.Error(), "returned empty output") {
		t.Fatalf("expected empty output failure, got %v", err)
	}
}

func TestFindConfiguredCLIBinaries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixtures are POSIX-only")
	}
	dir := t.TempDir()
	claude := writeExecutable(t, dir, "claude-configured", "#!/bin/sh\necho ok\n")
	gemini := writeExecutable(t, dir, "gemini-configured", "#!/bin/sh\necho ok\n")
	codex := writeExecutable(t, dir, "codex-configured", "#!/bin/sh\necho ok\n")

	cfg := &config.Config{ClaudeCLIPath: claude, GeminiCLIPath: gemini, CodexCLIPath: codex}
	if got, err := FindClaudeBinary(cfg); err != nil || got != claude {
		t.Fatalf("FindClaudeBinary = %q, %v", got, err)
	}
	if got, err := FindGeminiCLIBinary(cfg); err != nil || got != gemini {
		t.Fatalf("FindGeminiCLIBinary = %q, %v", got, err)
	}
	if got, err := FindCodexBinary(cfg); err != nil || got != codex {
		t.Fatalf("FindCodexBinary = %q, %v", got, err)
	}
}

func TestFindCLIBinariesFromPATHAndErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixtures are POSIX-only")
	}
	dir := t.TempDir()
	_ = writeExecutable(t, dir, "claude", "#!/bin/sh\necho ok\n")
	_ = writeExecutable(t, dir, "gemini", "#!/bin/sh\necho ok\n")
	_ = writeExecutable(t, dir, "codex", "#!/bin/sh\necho ok\n")
	t.Setenv("PATH", dir)
	t.Setenv("HOME", t.TempDir())

	if got, err := FindClaudeBinary(&config.Config{}); err != nil || filepath.Base(got) != "claude" {
		t.Fatalf("FindClaudeBinary from PATH = %q, %v", got, err)
	}
	if got, err := FindGeminiCLIBinary(&config.Config{}); err != nil || filepath.Base(got) != "gemini" {
		t.Fatalf("FindGeminiCLIBinary from PATH = %q, %v", got, err)
	}
	if got, err := FindCodexBinary(&config.Config{}); err != nil || filepath.Base(got) != "codex" {
		t.Fatalf("FindCodexBinary from PATH = %q, %v", got, err)
	}

	missing := filepath.Join(dir, "missing")
	if _, err := FindClaudeBinary(&config.Config{ClaudeCLIPath: missing}); err == nil || !strings.Contains(err.Error(), "configured path") {
		t.Fatalf("expected missing configured claude error, got %v", err)
	}
}

func TestCallAndStreamConfiguredCLIs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixtures are POSIX-only")
	}
	dir := t.TempDir()
	echoScript := "#!/bin/sh\necho generated from \"$1\" \"$2\"\n"
	streamScript := "#!/bin/sh\nprintf streamed-output\n"
	claude := writeExecutable(t, dir, "claude", echoScript)
	gemini := writeExecutable(t, dir, "gemini", echoScript)
	codex := writeExecutable(t, dir, "codex", streamScript)

	cfg := &config.Config{
		ClaudeCLIPath: claude,
		GeminiCLIPath: gemini,
		CodexCLIPath:  codex,
	}
	for name, call := range map[string]func(context.Context, *config.Config, string, string) (string, error){
		"claude": CallClaudeCLI,
		"gemini": CallGeminiCLI,
		"codex":  CallCodexCLI,
	} {
		got, err := call(context.Background(), cfg, "system", "diff")
		if err != nil {
			t.Fatalf("%s call failed: %v", name, err)
		}
		if strings.TrimSpace(got) == "" {
			t.Fatalf("%s call returned empty output", name)
		}
	}

	for name, stream := range map[string]func(context.Context, *config.Config, string, string, io.Writer) (string, error){
		"claude": StreamClaudeCLI,
		"gemini": StreamGeminiCLI,
		"codex":  StreamCodexCLI,
	} {
		var buf bytes.Buffer
		got, err := stream(context.Background(), cfg, "system", "diff", &buf)
		if err != nil {
			t.Fatalf("%s stream failed: %v", name, err)
		}
		if strings.TrimSpace(got) == "" || strings.TrimSpace(buf.String()) != got {
			t.Fatalf("unexpected %s streamed output: got=%q writer=%q", name, got, buf.String())
		}
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
