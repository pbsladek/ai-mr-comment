package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func dummyChatFn(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
	if strings.Contains(diffContent, "fail") {
		return "", errors.New("forced error")
	}
	return "mocked comment", nil
}

func TestNewRootCmd_DebugFlag(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--debug", "--file=testdata/simple.diff", "--provider=openai"})

	origStdout := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()

	_ = w.Close()
	os.Stdout = origStdout

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestNewRootCmd_UnsupportedProvider(t *testing.T) {
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--file=testdata/diff.txt", "--provider=invalid"})

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unsupported provider") {
		t.Fatalf("expected unsupported provider error, got %v", err)
	}
}

func TestNewRootCmd_ChatFnError(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--file=testdata/diff.txt", "--provider=openai"})

	_ = os.WriteFile("testdata/fail.txt", []byte("this should fail"), 0644)
	defer func() { _ = os.Remove("testdata/fail.txt") }()

	cmd.SetArgs([]string{"--file=testdata/fail.txt", "--provider=openai"})

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "forced error") {
		t.Fatalf("expected chat error, got %v", err)
	}
}

func TestNewRootCmd_OutputToFile(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	outputFile := "testdata/output.txt"
	defer func() { _ = os.Remove(outputFile) }()

	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--file=testdata/diff.txt", "--provider=openai", "--output=" + outputFile})

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("expected output file, got error %v", err)
	}
	if !strings.Contains(string(data), "mocked comment") {
		t.Fatalf("expected mocked comment in file")
	}
}

func TestNewRootCmd_FileNotFound(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--file=testdata/doesnotexist.diff", "--provider=openai"})

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "no such file or directory") {
		t.Fatalf("expected file not found error, got %v", err)
	}
}

func TestNewRootCmd_EmptyDiff(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	cmd := newRootCmd(func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
		return "", nil
	})
	cmd.SetArgs([]string{"--file=testdata/diff.txt", "--provider=openai"})

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestNewRootCmd_DebugOnly(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	cmd := newRootCmd(func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
		t.Fatalf("chatFn should not be called in debug mode")
		return "", nil
	})
	cmd.SetArgs([]string{"--file=testdata/diff.txt", "--provider=openai", "--debug"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err != nil {

		t.Fatalf("expected no error, got %v", err)
	}
}

func TestNewRootCmd_MissingOpenAIKey(t *testing.T) {
	// Ensure no config file is found and no env vars are set
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("AI_MR_COMMENT_OPENAI_API_KEY", "")

	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--file=testdata/diff.txt", "--provider=openai"})

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "missing OpenAI API key") {
		t.Fatalf("expected missing API key error, got %v", err)
	}
}

func TestNewRootCmd_MissingAnthropicKey(t *testing.T) {
	// Ensure no config file is found and no env vars are set
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("AI_MR_COMMENT_ANTHROPIC_API_KEY", "")

	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--file=testdata/diff.txt", "--provider=anthropic"})

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "missing Anthropic API key") {
		t.Fatalf("expected missing API key error, got %v", err)
	}
}

func TestNewRootCmd_TemplateFlag(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--file=testdata/diff.txt", "--provider=openai", "--template=nonexistent"})

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error (template falls back to default), got %v", err)
	}
}

func TestNewRootCmd_GitDiffPath(t *testing.T) {
	// Skip if HEAD^ is unavailable (shallow clone or no prior commit).
	if err := exec.Command("git", "rev-parse", "HEAD^").Run(); err != nil {
		t.Skip("skipping: HEAD^ not available")
	}
	t.Setenv("OPENAI_API_KEY", "dummy")
	alwaysOkFn := func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
		return "ok", nil
	}
	cmd := newRootCmd(alwaysOkFn)
	// Use HEAD^..HEAD so there's always a non-empty diff in the repo.
	cmd.SetArgs([]string{"--provider=openai", "--commit=HEAD^..HEAD"})

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestNewRootCmd_OllamaConnectionRefused(t *testing.T) {
	chatFn := func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
		return "", fmt.Errorf("Post \"http://localhost:99999/api/generate\": dial tcp: connection refused")
	}
	cmd := newRootCmd(chatFn)
	cmd.SetArgs([]string{"--file=testdata/diff.txt", "--provider=ollama"})

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to connect to Ollama") {
		t.Errorf("expected Ollama connection error, got %q", err.Error())
	}
}

func TestNewRootCmd_DebugTokenEstimationError(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "dummy")
	cmd := newRootCmd(dummyChatFn)
	// Use gemini provider in debug mode — without a real API, the SDK token counting will fail
	// and trigger the fallback path
	cmd.SetArgs([]string{"--file=testdata/diff.txt", "--provider=gemini", "--debug"})

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error (should fallback to heuristic), got %v", err)
	}
}

func TestGetModelName(t *testing.T) {
	tests := []struct {
		provider ApiProvider
		cfg      Config
		expected string
	}{
		{OpenAI, Config{Provider: OpenAI, OpenAIModel: "gpt-4o"}, "gpt-4o"},
		{Anthropic, Config{Provider: Anthropic, AnthropicModel: "claude-3"}, "claude-3"},
		{Gemini, Config{Provider: Gemini, GeminiModel: "gemini-2.5-flash"}, "gemini-2.5-flash"},
		{Ollama, Config{Provider: Ollama, OllamaModel: "llama3"}, "llama3"},
		{"unknown", Config{Provider: "unknown"}, "unknown"},
	}
	for _, tc := range tests {
		t.Run(string(tc.provider), func(t *testing.T) {
			result := getModelName(&tc.cfg)
			if result != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestNewRootCmd_StagedFlag(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--staged", "--file=testdata/diff.txt", "--provider=openai"})

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestNewRootCmd_StagedAndCommitMutuallyExclusive(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--staged", "--commit=HEAD", "--file=testdata/diff.txt", "--provider=openai"})

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive error, got %v", err)
	}
}

func TestNewRootCmd_ClipboardFlag(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	for _, val := range []string{"description", "comment", "title", "all"} {
		t.Run(val, func(t *testing.T) {
			callCount := 0
			fn := func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
				callCount++
				if callCount == 1 {
					return "mocked comment", nil
				}
				return "mocked title", nil
			}
			cmd := newRootCmd(fn)
			cmd.SetArgs([]string{"--clipboard=" + val, "--title", "--file=testdata/diff.txt", "--provider=openai"})
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			// Clipboard may fail in headless CI environments; that's a warning, not an error
			err := cmd.Execute()
			if err != nil {
				t.Fatalf("expected no error for --clipboard=%s, got %v", val, err)
			}
		})
	}
}

func TestNewRootCmd_ClipboardFlag_InvalidValue(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	var errBuf strings.Builder
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--clipboard=invalid", "--file=testdata/diff.txt", "--provider=openai"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(&errBuf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error (warning only), got %v", err)
	}
	if !strings.Contains(errBuf.String(), "unknown --clipboard value") {
		t.Errorf("expected warning about unknown clipboard value, got: %s", errBuf.String())
	}
}

func TestNewRootCmd_TitleFlag(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	callCount := 0
	trackingFn := func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
		callCount++
		if callCount == 1 {
			return "mocked comment", nil
		}
		return "Add mocked feature", nil
	}

	var buf strings.Builder
	cmd := newRootCmd(trackingFn)
	cmd.SetArgs([]string{"--title", "--file=testdata/diff.txt", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 chatFn calls (comment + title), got %d", callCount)
	}
	out := buf.String()
	if !strings.Contains(out, "Add mocked feature") {
		t.Error("expected title in output")
	}
	if !strings.Contains(out, "── Title ──") {
		t.Error("expected title section header in output")
	}
	if !strings.Contains(out, "── Description ──") {
		t.Error("expected description section header in output")
	}
}

func TestNewRootCmd_TitleFlagJSON(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	callCount := 0
	trackingFn := func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
		callCount++
		if callCount == 1 {
			return "mocked comment", nil
		}
		return "Add mocked feature", nil
	}

	var buf strings.Builder
	cmd := newRootCmd(trackingFn)
	cmd.SetArgs([]string{"--title", "--format=json", "--file=testdata/diff.txt", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal([]byte(buf.String()), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", buf.String())
	}
	if result["title"] != "Add mocked feature" {
		t.Errorf("expected title 'Add mocked feature', got %q", result["title"])
	}
	if result["description"] != "mocked comment" {
		t.Errorf("expected description 'mocked comment', got %q", result["description"])
	}
	if result["comment"] != "mocked comment" {
		t.Errorf("expected comment 'mocked comment' (backwards compat), got %q", result["comment"])
	}
}

func TestCompletionCommand(t *testing.T) {
	// Capture os.Stdout since GenBashCompletionV2 writes directly to os.Stdout
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"completion", "bash"})
	cmd.SetErr(io.Discard)
	err := cmd.Execute()

	_ = w.Close()
	os.Stdout = origStdout

	var buf strings.Builder
	_, _ = io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(buf.String(), "ai-mr-comment") {
		t.Error("expected completion output to contain 'ai-mr-comment'")
	}
}

func TestNewRootCmd_ExcludeFlag(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--exclude=*.md", "--file=testdata/diff.txt", "--provider=openai"})

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestNewRootCmd_FormatJSON(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	var buf strings.Builder
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--format=json", "--file=testdata/diff.txt", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal([]byte(buf.String()), &result); err != nil {
		t.Fatalf("expected valid JSON output, got error: %v\noutput: %s", err, buf.String())
	}
	if result["description"] != "mocked comment" {
		t.Errorf("expected description 'mocked comment', got %q", result["description"])
	}
	if result["comment"] != "mocked comment" {
		t.Errorf("expected comment 'mocked comment' (backwards compat), got %q", result["comment"])
	}
	if result["provider"] == "" {
		t.Error("expected non-empty provider in JSON output")
	}
	if _, ok := result["title"]; !ok {
		t.Error("expected title field present in JSON output")
	}
}

func TestNewRootCmd_FormatInvalid(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--format=xml", "--file=testdata/diff.txt", "--provider=openai"})

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unsupported format") {
		t.Fatalf("expected unsupported format error, got %v", err)
	}
}

func TestNewRootCmd_SmartChunk(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	callCount := 0
	trackingFn := func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
		callCount++
		return "mocked comment", nil
	}
	cmd := newRootCmd(trackingFn)
	cmd.SetArgs([]string{"--smart-chunk", "--file=testdata/diff.txt", "--provider=openai"})

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if callCount == 0 {
		t.Error("expected chatFn to be called at least once")
	}
}

// TestStreaming_NonTTYUsesBuffered confirms that when stdout is not a TTY (as in
// all test runs), the buffered chatFn path is used and the output is correct.
func TestStreaming_NonTTYUsesBuffered(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	called := 0
	fn := func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
		called++
		return "buffered comment", nil
	}
	var buf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--file=testdata/diff.txt", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called != 1 {
		t.Errorf("expected chatFn called once, got %d", called)
	}
	out := buf.String()
	if !strings.Contains(out, "buffered comment") {
		t.Errorf("expected buffered comment in output, got: %s", out)
	}
	if !strings.Contains(out, "── Description ──") {
		t.Errorf("expected description section header in output, got: %s", out)
	}
}

// TestStreaming_JSONFormatSkipsStream confirms that --format json never streams
// (needs the full response to encode) and produces valid JSON.
func TestStreaming_JSONFormatSkipsStream(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	var buf strings.Builder
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--format=json", "--file=testdata/diff.txt", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result map[string]string
	if err := json.Unmarshal([]byte(buf.String()), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", buf.String())
	}
	if result["comment"] != "mocked comment" {
		t.Errorf("expected 'mocked comment', got %q", result["comment"])
	}
}

// TestStreaming_SmartChunkSkipsStream confirms --smart-chunk always uses the
// buffered chatFn path for both per-file summarise calls and the final call.
func TestStreaming_SmartChunkSkipsStream(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	called := 0
	fn := func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
		called++
		return "chunk result", nil
	}
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--smart-chunk", "--file=testdata/diff.txt", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called == 0 {
		t.Error("expected chatFn to be called at least once")
	}
}

func TestInitConfig_WritesFile(t *testing.T) {
	dest := t.TempDir() + "/test-config.toml"

	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"init-config", "--output=" + dest})
	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("expected config file to exist, got %v", err)
	}
	if !strings.Contains(string(data), "provider") {
		t.Error("expected config to contain 'provider'")
	}
	if !strings.Contains(string(data), "openai_model") {
		t.Error("expected config to contain 'openai_model'")
	}
	if !strings.Contains(buf.String(), dest) {
		t.Errorf("expected stdout to mention destination path, got %q", buf.String())
	}
}

func TestInitConfig_RefusesOverwrite(t *testing.T) {
	dest := t.TempDir() + "/existing.toml"
	if err := os.WriteFile(dest, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}

	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"init-config", "--output=" + dest})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected already-exists error, got %v", err)
	}

	// Existing file must be untouched.
	data, _ := os.ReadFile(dest)
	if string(data) != "existing" {
		t.Error("expected existing file to be unchanged")
	}
}

func TestInitConfig_DefaultPath(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"init-config"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	data, err := os.ReadFile(tmpHome + "/.ai-mr-comment.toml")
	if err != nil {
		t.Fatalf("expected default config file at ~/.ai-mr-comment.toml, got %v", err)
	}
	if !strings.Contains(string(data), "ollama_endpoint") {
		t.Error("expected config to contain 'ollama_endpoint'")
	}
}

func TestInitConfig_ContentIsValidTOML(t *testing.T) {
	dest := t.TempDir() + "/config.toml"

	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"init-config", "--output=" + dest})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Load the generated file through loadConfigWith to verify it parses cleanly.
	v := newViperFromFile(dest)
	_, err := loadConfigWith(v)
	if err != nil {
		t.Fatalf("generated config failed to parse: %v", err)
	}
}

func TestNewRootCmd_MissingGeminiKey(t *testing.T) {
	// Ensure no config file is found and no env vars are set
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("AI_MR_COMMENT_GEMINI_API_KEY", "")

	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--file=testdata/diff.txt", "--provider=gemini"})

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "missing Gemini API key") {
		t.Fatalf("expected missing API key error, got %v", err)
	}
}

func TestVerboseFlag_BasicOutput(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	var errBuf strings.Builder
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--verbose", "--file=testdata/diff.txt", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	stderr := errBuf.String()
	checks := []string{
		"[debug] config:",
		"provider=openai",
		"[debug] diff: source=",
		"[debug] diff: lines before truncation=",
		"[debug] template:",
		"[debug] streaming:",
		"[debug] output:",
	}
	for _, want := range checks {
		if !strings.Contains(stderr, want) {
			t.Errorf("expected stderr to contain %q\nfull stderr:\n%s", want, stderr)
		}
	}
}

func TestVerboseFlag_NoOutputWithoutFlag(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	var errBuf strings.Builder
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--file=testdata/diff.txt", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if strings.Contains(errBuf.String(), "[debug]") {
		t.Errorf("expected no debug output without --verbose, got:\n%s", errBuf.String())
	}
}

func TestVerboseFlag_SmartChunk(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	var errBuf strings.Builder
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--verbose", "--smart-chunk", "--file=testdata/multiple-files.diff", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, "[debug] smart-chunk:") {
		t.Errorf("expected smart-chunk debug lines, got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "files=2") {
		t.Errorf("expected files=2 in smart-chunk debug, got:\n%s", stderr)
	}
}

func TestVerboseFlag_DoesNotInterfereWithDebugFlag(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	var outBuf, errBuf strings.Builder
	cmd := newRootCmd(func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		t.Fatal("chatFn should not be called in --debug mode")
		return "", nil
	})
	cmd.SetArgs([]string{"--debug", "--verbose", "--file=testdata/diff.txt", "--provider=openai"})
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !strings.Contains(outBuf.String(), "Token & Cost Estimation:") {
		t.Errorf("expected token estimation output on stdout, got:\n%s", outBuf.String())
	}
	if !strings.Contains(errBuf.String(), "[debug] config:") {
		t.Errorf("expected verbose debug lines on stderr, got:\n%s", errBuf.String())
	}
}

func TestVerboseFlag_ConfigFilePath(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	// Point HOME at a tempdir so no real config file is found.
	t.Setenv("HOME", t.TempDir())

	var errBuf strings.Builder
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--verbose", "--file=testdata/diff.txt", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !strings.Contains(errBuf.String(), "file=(none)") {
		t.Errorf("expected config file=(none) in debug output, got:\n%s", errBuf.String())
	}
}

func TestVerboseFlag_ResponseTiming(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	var errBuf strings.Builder
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--verbose", "--file=testdata/diff.txt", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	stderr := errBuf.String()
	for _, want := range []string{"[debug] api:", "ms", "chars=", "lines="} {
		if !strings.Contains(stderr, want) {
			t.Errorf("expected stderr to contain %q\nfull stderr:\n%s", want, stderr)
		}
	}
}

func TestVerboseFlag_DiffBytes(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	var errBuf strings.Builder
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--verbose", "--file=testdata/diff.txt", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !strings.Contains(errBuf.String(), "bytes=") {
		t.Errorf("expected bytes= in diff source debug line, got:\n%s", errBuf.String())
	}
}

func TestNewRootCmd_FormatJSON_AutoTitle(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	callCount := 0
	trackingFn := func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
		callCount++
		if callCount == 1 {
			return "mocked description", nil
		}
		return "Add mocked feature", nil
	}

	var buf strings.Builder
	cmd := newRootCmd(trackingFn)
	// Note: no --title flag — title is implied by --format=json
	cmd.SetArgs([]string{"--format=json", "--file=testdata/diff.txt", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if callCount != 2 {
		t.Errorf("expected 2 chatFn calls (description + auto-title), got %d", callCount)
	}

	var result map[string]string
	if err := json.Unmarshal([]byte(buf.String()), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", buf.String())
	}
	if result["title"] != "Add mocked feature" {
		t.Errorf("expected auto-generated title 'Add mocked feature', got %q", result["title"])
	}
	if result["description"] != "mocked description" {
		t.Errorf("expected description 'mocked description', got %q", result["description"])
	}
	if result["comment"] != "mocked description" {
		t.Errorf("expected comment 'mocked description' (backwards compat), got %q", result["comment"])
	}
}
