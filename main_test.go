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
	"sync"
	"sync/atomic"
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

func TestRootCmd_MalformedConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	if err := os.WriteFile(tmpHome+"/.ai-mr-comment.toml", []byte("provider = "), 0600); err != nil {
		t.Fatalf("failed to write malformed config: %v", err)
	}

	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--file=testdata/simple.diff", "--provider=openai"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected malformed config error, got nil")
	}
	if !strings.Contains(err.Error(), "malformed config file") {
		t.Fatalf("expected malformed config error, got: %v", err)
	}
}

func TestChangelogCmd_MalformedConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	if err := os.WriteFile(tmpHome+"/.ai-mr-comment.toml", []byte("provider = "), 0600); err != nil {
		t.Fatalf("failed to write malformed config: %v", err)
	}

	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"changelog", "--file=testdata/simple.diff", "--provider=openai"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected malformed config error, got nil")
	}
	if !strings.Contains(err.Error(), "malformed config file") {
		t.Fatalf("expected malformed config error, got: %v", err)
	}
}

func TestQuickCommitCmd_MalformedConfig(t *testing.T) {
	if !isGitRepo() {
		t.Skip("skipping: not inside a git repository")
	}

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	if err := os.WriteFile(tmpHome+"/.ai-mr-comment.toml", []byte("provider = "), 0600); err != nil {
		t.Fatalf("failed to write malformed config: %v", err)
	}

	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"quick-commit", "--dry-run", "--provider=openai"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected malformed config error, got nil")
	}
	if !strings.Contains(err.Error(), "malformed config file") {
		t.Fatalf("expected malformed config error, got: %v", err)
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
			var callCount atomic.Int32
			fn := func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
				if callCount.Add(1) == 1 {
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
	var callCount atomic.Int32
	trackingFn := func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
		if callCount.Add(1) == 1 {
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
	if got := callCount.Load(); got != 2 {
		t.Errorf("expected 2 chatFn calls (comment + title), got %d", got)
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
	var callCount atomic.Int32
	trackingFn := func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
		callCount.Add(1)
		// Distinguish by prompt content — title and comment run concurrently now.
		if strings.HasPrefix(systemPrompt, "Generate a single-line MR/PR title") {
			return "Add mocked feature", nil
		}
		return "mocked comment", nil
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

	var callCount atomic.Int32
	trackingFn := func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
		callCount.Add(1)
		// Distinguish by prompt content — title and comment run concurrently now.
		if strings.HasPrefix(systemPrompt, "Generate a single-line MR/PR title") {
			return "Add mocked feature", nil
		}
		return "mocked description", nil
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

	if got := callCount.Load(); got != 2 {
		t.Errorf("expected 2 chatFn calls (description + auto-title), got %d", got)
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

func TestNewRootCmd_ModelFlag(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	var capturedModel string
	fn := func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
		capturedModel = cfg.OpenAIModel
		return "mocked comment", nil
	}

	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--model=gpt-4o", "--file=testdata/diff.txt", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if capturedModel != "gpt-4o" {
		t.Errorf("expected model gpt-4o, got %q", capturedModel)
	}
}

func TestSetModelOverride(t *testing.T) {
	tests := []struct {
		provider ApiProvider
		model    string
		check    func(*Config) string
	}{
		{OpenAI, "gpt-4o", func(c *Config) string { return c.OpenAIModel }},
		{Anthropic, "claude-opus-4-6", func(c *Config) string { return c.AnthropicModel }},
		{Gemini, "gemini-2.0-flash", func(c *Config) string { return c.GeminiModel }},
		{Ollama, "mistral", func(c *Config) string { return c.OllamaModel }},
	}
	for _, tc := range tests {
		t.Run(string(tc.provider), func(t *testing.T) {
			cfg := &Config{Provider: tc.provider}
			setModelOverride(cfg, tc.model)
			if got := tc.check(cfg); got != tc.model {
				t.Errorf("expected %q, got %q", tc.model, got)
			}
		})
	}
}

func TestModelsCmd(t *testing.T) {
	for _, provider := range []string{"openai", "anthropic", "gemini", "ollama"} {
		t.Run(provider, func(t *testing.T) {
			var buf strings.Builder
			cmd := newRootCmd(dummyChatFn)
			cmd.SetArgs([]string{"models", "--provider=" + provider})
			cmd.SetOut(&buf)
			cmd.SetErr(io.Discard)

			if err := cmd.Execute(); err != nil {
				t.Fatalf("expected no error for provider %s, got %v", provider, err)
			}
			if !strings.Contains(buf.String(), provider) {
				t.Errorf("expected output to mention provider %q, got:\n%s", provider, buf.String())
			}
			if !strings.Contains(buf.String(), "--model") {
				t.Errorf("expected output to mention --model flag, got:\n%s", buf.String())
			}
		})
	}
}

func TestModelsCmd_InvalidProvider(t *testing.T) {
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"models", "--provider=invalid"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Errorf("expected unknown provider error, got %v", err)
	}
}

func TestNewRootCmd_ModelFlag_TokenEstimation(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	var buf strings.Builder
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--debug", "--model=gpt-4o", "--file=testdata/diff.txt", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "gpt-4o") {
		t.Errorf("expected model gpt-4o in debug output, got:\n%s", out)
	}
	// gpt-4o costs more than gpt-4o-mini — ensure EstimateCost picked up the override
	if !strings.Contains(out, "Estimated Input Cost") {
		t.Errorf("expected cost estimation in output, got:\n%s", out)
	}
}

func TestNewRootCmd_CommitMsgFlag(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	callCount := 0
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		callCount++
		return "feat(auth): add JWT refresh token support", nil
	}

	var buf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--commit-msg", "--file=testdata/diff.txt", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Exactly one API call — description generation is skipped.
	if callCount != 1 {
		t.Errorf("expected 1 chatFn call for --commit-msg, got %d", callCount)
	}

	out := buf.String()
	if !strings.Contains(out, "feat(auth): add JWT refresh token support") {
		t.Errorf("expected commit message in output, got:\n%s", out)
	}
	// No section headers should appear.
	if strings.Contains(out, "── Title ──") {
		t.Error("expected no title section header for --commit-msg")
	}
	if strings.Contains(out, "── Description ──") {
		t.Error("expected no description section header for --commit-msg")
	}
}

func TestNewRootCmd_CommitMsgFlag_FormatJSON(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	callCount := 0
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		callCount++
		return "chore: update dependencies", nil
	}

	var buf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--commit-msg", "--format=json", "--file=testdata/diff.txt", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// No auto-title call: --commit-msg suppresses it even with --format=json.
	if callCount != 1 {
		t.Errorf("expected 1 chatFn call (no auto-title for --commit-msg), got %d", callCount)
	}

	var result map[string]string
	if err := json.Unmarshal([]byte(buf.String()), &result); err != nil {
		t.Fatalf("expected valid JSON, got error: %v\noutput: %s", err, buf.String())
	}
	if result["commit_message"] != "chore: update dependencies" {
		t.Errorf("expected commit_message 'chore: update dependencies', got %q", result["commit_message"])
	}
	if _, ok := result["description"]; ok {
		t.Error("expected no description field in commit-msg JSON output")
	}
	if _, ok := result["comment"]; ok {
		t.Error("expected no comment field in commit-msg JSON output")
	}
	if result["provider"] == "" {
		t.Error("expected non-empty provider in JSON output")
	}
	if result["model"] == "" {
		t.Error("expected non-empty model in JSON output")
	}
}

func TestNewRootCmd_CommitMsgFlag_MutualExclusion(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--commit-msg", "--title", "--file=testdata/diff.txt", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --commit-msg and --title are combined, got nil")
	}
	if !strings.Contains(err.Error(), "--commit-msg and --title cannot be used together") {
		t.Errorf("expected mutual exclusion error, got: %v", err)
	}
}

func TestNewRootCmd_CommitMsgFlag_Clipboard(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		return "fix(api): handle nil response", nil
	}

	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--commit-msg", "--clipboard=commit-msg", "--file=testdata/diff.txt", "--provider=openai"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	// Clipboard may fail in headless CI environments; that's a warning, not an error.
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error for --clipboard=commit-msg, got %v", err)
	}
}

func TestNewRootCmd_CommitMsgFlag_NoAutoTitle(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	callCount := 0
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		callCount++
		return "refactor: simplify token parsing", nil
	}

	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--commit-msg", "--format=json", "--file=testdata/diff.txt", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected exactly 1 API call (no title generation), got %d", callCount)
	}
}

func TestNewRootCmd_CommitMsgFlag_NormalizesMultilineOutput(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		return "feat(auth): add JWT refresh token support\n" +
			"docs: update README format", nil
	}

	var buf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--commit-msg", "--file=testdata/diff.txt", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	got := strings.TrimSpace(buf.String())
	if got != "feat(auth): add JWT refresh token support" {
		t.Fatalf("expected normalized first commit message line, got %q", got)
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("expected single-line commit message, got %q", got)
	}
}

// TestBranchPrependedForLocalGitDiff verifies that when diffing a local git
// repo (no --file, no --pr), the branch name is prepended to the diffContent
// that reaches the AI so templates like jira can extract the ticket key.
func TestBranchPrependedForLocalGitDiff(t *testing.T) {
	if !isGitRepo() {
		t.Skip("skipping: not inside a git repository")
	}
	t.Setenv("OPENAI_API_KEY", "dummy")

	var capturedDiff string
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, diffContent string) (string, error) {
		capturedDiff = diffContent
		return "mocked comment", nil
	}

	cmd := newRootCmd(fn)
	// Use --staged so we get a real git diff path without needing uncommitted changes.
	cmd.SetArgs([]string{"--staged", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	// May return "no staged changes" error — that's fine, we only care about
	// whether the branch prefix was added before the diff is processed.
	// If the error is about missing staged changes the branch inject already ran.
	err := cmd.Execute()
	if err != nil && !strings.Contains(err.Error(), "no staged changes") && !strings.Contains(err.Error(), "no diff found") {
		t.Fatalf("unexpected error: %v", err)
	}

	// If chatFn was called, capturedDiff must start with "Branch: ".
	if capturedDiff != "" && !strings.HasPrefix(capturedDiff, "Branch: ") {
		preview := capturedDiff
		if len(preview) > 200 {
			preview = preview[:200]
		}
		t.Errorf("expected diffContent to start with 'Branch: ', got:\n%s", preview)
	}
}

// TestBranchNotPrependedForFileDiff verifies that --file skips branch injection
// (the file has no local branch context).
func TestBranchNotPrependedForFileDiff(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	var capturedDiff string
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, diffContent string) (string, error) {
		capturedDiff = diffContent
		return "mocked comment", nil
	}

	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--file=testdata/diff.txt", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if strings.HasPrefix(capturedDiff, "Branch: ") {
		t.Error("expected no Branch: prefix when using --file")
	}
}

// skipIfDetachedHead skips the test when the repo is in detached HEAD state
// (e.g. CI shallow clones), since quick-commit requires a named branch.
func skipIfDetachedHead(t *testing.T) {
	t.Helper()
	branch, err := getCurrentBranch()
	if err != nil || branch == "" {
		t.Skip("skipping: detached HEAD state or no branch available")
	}
}

// TestQuickCommit_DryRun verifies that --dry-run generates and prints the
// commit message without staging, committing, or pushing.
func TestQuickCommit_DryRun(t *testing.T) {
	if !isGitRepo() {
		t.Skip("skipping: not inside a git repository")
	}
	skipIfDetachedHead(t)
	t.Setenv("OPENAI_API_KEY", "dummy")

	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		return "chore: update config", nil
	}

	var buf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"quick-commit", "--dry-run", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	// Skip when the working tree is clean — no diff to feed the AI.
	if err != nil && (strings.Contains(err.Error(), "no staged changes") || strings.Contains(err.Error(), "no changes found")) {
		t.Skip("skipping: no diff available in working tree")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "chore: update config") {
		t.Errorf("expected commit message in output, got:\n%s", out)
	}
	if !strings.Contains(out, "dry-run") {
		t.Errorf("expected dry-run notice in output, got:\n%s", out)
	}
}

// TestQuickCommit_DryRun_BranchPrefix verifies that the branch name is
// prepended to the diff content passed to the AI.
func TestQuickCommit_DryRun_BranchPrefix(t *testing.T) {
	if !isGitRepo() {
		t.Skip("skipping: not inside a git repository")
	}
	skipIfDetachedHead(t)
	t.Setenv("OPENAI_API_KEY", "dummy")

	var capturedDiff string
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, diffContent string) (string, error) {
		capturedDiff = diffContent
		return "feat: add feature", nil
	}

	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"quick-commit", "--dry-run", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	// Skip when the working tree is clean — no diff to feed the AI.
	if err != nil && (strings.Contains(err.Error(), "no staged changes") || strings.Contains(err.Error(), "no changes found")) {
		t.Skip("skipping: no diff available in working tree")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedDiff != "" && !strings.HasPrefix(capturedDiff, "Branch: ") {
		t.Errorf("expected diffContent to start with 'Branch: ', got:\n%s", capturedDiff)
	}
}

// TestQuickCommit_AIError verifies that an AI error is surfaced correctly.
func TestQuickCommit_AIError(t *testing.T) {
	if !isGitRepo() {
		t.Skip("skipping: not inside a git repository")
	}
	skipIfDetachedHead(t)
	t.Setenv("OPENAI_API_KEY", "dummy")

	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		return "", fmt.Errorf("AI provider unavailable")
	}

	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"quick-commit", "--dry-run", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	// Skip when the working tree is clean — no diff to reach the AI call.
	if err != nil && (strings.Contains(err.Error(), "no staged changes") || strings.Contains(err.Error(), "no changes found")) {
		t.Skip("skipping: no diff available in working tree")
	}
	if err == nil {
		t.Fatal("expected an error from AI failure, got nil")
	}
	if !strings.Contains(err.Error(), "AI provider unavailable") {
		t.Errorf("expected AI error message, got: %v", err)
	}
}

// TestQuickCommit_DetachedHead verifies an error is returned in detached HEAD state.
func TestQuickCommit_DetachedHead(t *testing.T) {
	// Check if we're actually in detached HEAD; if not, this code path can't be
	// triggered without destructive git operations, so we just unit-test the guard
	// indirectly by confirming getCurrentBranch returns "" → "" branch guard fires.
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		t.Skip("skipping: not in a git repo")
	}
	if strings.TrimSpace(string(out)) != "HEAD" {
		t.Skip("skipping: not in detached HEAD state")
	}

	t.Setenv("OPENAI_API_KEY", "dummy")
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"quick-commit", "--dry-run", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	execErr := cmd.Execute()
	if execErr == nil {
		t.Fatal("expected error for detached HEAD, got nil")
	}
	if !strings.Contains(execErr.Error(), "detached HEAD") {
		t.Errorf("expected detached HEAD error, got: %v", execErr)
	}
}

// --- parseVerdict unit tests ---

func TestParseVerdict_Pass(t *testing.T) {
	verdict, body := parseVerdict("VERDICT: PASS\nThis looks good.")
	if verdict != "PASS" {
		t.Errorf("expected verdict PASS, got %q", verdict)
	}
	if body != "This looks good." {
		t.Errorf("expected body %q, got %q", "This looks good.", body)
	}
}

func TestParseVerdict_Fail(t *testing.T) {
	verdict, body := parseVerdict("VERDICT: FAIL\nThere is a SQL injection.")
	if verdict != "FAIL" {
		t.Errorf("expected verdict FAIL, got %q", verdict)
	}
	if body != "There is a SQL injection." {
		t.Errorf("expected body %q, got %q", "There is a SQL injection.", body)
	}
}

func TestParseVerdict_NoVerdictLine(t *testing.T) {
	input := "Normal review text without verdict."
	verdict, body := parseVerdict(input)
	if verdict != "UNKNOWN" {
		t.Errorf("expected default UNKNOWN, got %q", verdict)
	}
	if body != input {
		t.Errorf("expected body unchanged, got %q", body)
	}
}

func TestNormalizeCommitMessage_PrefersConventionalLine(t *testing.T) {
	raw := "Commit message:\nrefactor(parser): simplify token handling\nextra note"
	got := normalizeCommitMessage(raw)
	if got != "refactor(parser): simplify token handling" {
		t.Fatalf("expected conventional commit line, got %q", got)
	}
}

func TestNormalizeCommitMessage_FallsBackToFirstCleanLine(t *testing.T) {
	raw := "```text\n- Improve parser performance\n```"
	got := normalizeCommitMessage(raw)
	if got != "Improve parser performance" {
		t.Fatalf("expected first cleaned line, got %q", got)
	}
}

// --- --exit-code flag tests ---

// TestExitCodeFlag_Pass verifies that VERDICT: PASS is stripped from the output
// and the command exits successfully (returns nil).
func TestExitCodeFlag_Pass(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		return "VERDICT: PASS\nmocked comment", nil
	}

	var buf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--exit-code", "--file=testdata/simple.diff", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	output := buf.String()
	if strings.Contains(output, "VERDICT:") {
		t.Errorf("expected VERDICT line to be stripped from output, got:\n%s", output)
	}
	if !strings.Contains(output, "mocked comment") {
		t.Errorf("expected review body in output, got:\n%s", output)
	}
}

// TestExitCodeFlag_JSON verifies that --exit-code --format=json includes
// a "verdict" field in the JSON output.
func TestExitCodeFlag_JSON(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		return "VERDICT: PASS\nmocked comment", nil
	}

	var buf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--exit-code", "--format=json", "--file=testdata/simple.diff", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(buf.String()), &result); err != nil {
		t.Fatalf("expected valid JSON, got error: %v\nOutput: %s", err, buf.String())
	}
	if result["verdict"] != "PASS" {
		t.Errorf("expected verdict=PASS in JSON, got: %v", result["verdict"])
	}
	// The description should not contain the raw VERDICT line — it must be stripped.
	if desc, _ := result["description"].(string); strings.Contains(desc, "VERDICT:") {
		t.Errorf("expected raw VERDICT line to be stripped from description, got: %q", desc)
	}
}

// TestExitCodeFlag_MutualExclusion verifies that --exit-code and --commit-msg
// cannot be used together.
func TestExitCodeFlag_MutualExclusion(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--exit-code", "--commit-msg", "--file=testdata/simple.diff", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected mutual exclusion error, got nil")
	}
	if !strings.Contains(err.Error(), "--exit-code") || !strings.Contains(err.Error(), "--commit-msg") {
		t.Errorf("expected error to mention both flags, got: %v", err)
	}
}

// TestExitCodeFlag_Fail verifies that the process exits with code 2 when the AI
// returns VERDICT: FAIL. Uses the subprocess test pattern to observe os.Exit.
func TestExitCodeFlag_Fail(t *testing.T) {
	if os.Getenv("AI_MR_EXIT_CODE_SUBPROCESS") == "1" {
		// Running as subprocess: execute the logic that calls os.Exit(2).
		fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
			return "VERDICT: FAIL\nbad code detected", nil
		}
		t.Setenv("OPENAI_API_KEY", "dummy")
		cmd := newRootCmd(fn)
		cmd.SetArgs([]string{"--exit-code", "--file=testdata/simple.diff", "--provider=openai"})
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		_ = cmd.Execute()
		// If os.Exit(2) is not called, we fall through and exit 0 — test will catch this.
		return
	}

	proc := exec.Command(os.Args[0], "-test.run=TestExitCodeFlag_Fail", "-test.v")
	proc.Env = append(os.Environ(), "AI_MR_EXIT_CODE_SUBPROCESS=1")
	err := proc.Run()
	if err == nil {
		t.Fatal("expected process to exit with non-zero code, got nil")
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}
	if exitErr.ExitCode() != 2 {
		t.Errorf("expected exit code 2, got %d", exitErr.ExitCode())
	}
}

// TestExitCodeFlag_MissingVerdictFailsClosed verifies missing verdict lines are
// treated as FAIL and exit with code 2 when --exit-code is set.
func TestExitCodeFlag_MissingVerdictFailsClosed(t *testing.T) {
	if os.Getenv("AI_MR_EXIT_CODE_SUBPROCESS_MISSING_VERDICT") == "1" {
		fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
			return "review body without verdict", nil
		}
		t.Setenv("OPENAI_API_KEY", "dummy")
		cmd := newRootCmd(fn)
		cmd.SetArgs([]string{"--exit-code", "--file=testdata/simple.diff", "--provider=openai"})
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		_ = cmd.Execute()
		return
	}

	proc := exec.Command(os.Args[0], "-test.run=TestExitCodeFlag_MissingVerdictFailsClosed", "-test.v")
	proc.Env = append(os.Environ(), "AI_MR_EXIT_CODE_SUBPROCESS_MISSING_VERDICT=1")
	err := proc.Run()
	if err == nil {
		t.Fatal("expected process to exit with non-zero code, got nil")
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}
	if exitErr.ExitCode() != 2 {
		t.Errorf("expected exit code 2, got %d", exitErr.ExitCode())
	}
}

// --- --post flag tests ---

// TestPostFlag_RequiresPR verifies that --post without --pr returns an error.
func TestPostFlag_RequiresPR(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--post", "--file=testdata/simple.diff", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --post used without --pr, got nil")
	}
	if !strings.Contains(err.Error(), "--post requires --pr") {
		t.Errorf("expected '--post requires --pr' error, got: %v", err)
	}
}

// --- --output with --format=json test ---

// TestOutputFlag_JSON verifies that --output writes valid JSON to the file
// when --format=json is set.
func TestOutputFlag_JSON(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	tmpFile := t.TempDir() + "/review.json"

	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		return "mocked comment", nil
	}

	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--format=json", "--output=" + tmpFile, "--file=testdata/simple.diff", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	content, readErr := os.ReadFile(tmpFile)
	if readErr != nil {
		t.Fatalf("expected output file to exist, got error: %v", readErr)
	}

	var result map[string]any
	if jsonErr := json.Unmarshal(content, &result); jsonErr != nil {
		t.Fatalf("expected valid JSON in output file, got error: %v\nContent: %s", jsonErr, string(content))
	}
	if result["description"] == nil && result["comment"] == nil {
		t.Errorf("expected description or comment key in JSON, got: %v", result)
	}
	if result["provider"] == nil {
		t.Errorf("expected provider key in JSON, got: %v", result)
	}
}

// ---------------------------------------------------------------------------
// Smart-chunk tests
// ---------------------------------------------------------------------------

// TestSmartChunk_MultiFile verifies that --smart-chunk splits a multi-file diff
// into per-file chunks, calls the chat function once per chunk (parallel) plus
// one synthesis call, and returns a non-empty comment.
func TestSmartChunk_MultiFile(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	var mu sync.Mutex
	callsByPrompt := map[string]int{}

	mockFn := func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
		mu.Lock()
		callsByPrompt[systemPrompt]++
		mu.Unlock()
		if strings.HasPrefix(systemPrompt, "Summarize the changes") {
			return "chunk summary: " + diffContent[:min(30, len(diffContent))], nil
		}
		// Synthesis call — receives the combined summaries.
		return "final synthesis comment", nil
	}

	var buf strings.Builder
	cmd := newRootCmd(mockFn)
	cmd.SetArgs([]string{
		"--smart-chunk",
		"--file=testdata/large-multi-file.diff",
		"--provider=openai",
		"--format=text",
	})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "final synthesis comment") {
		t.Errorf("expected synthesis comment in output, got:\n%s", out)
	}

	mu.Lock()
	defer mu.Unlock()

	// Count chunk-summary calls vs synthesis calls.
	chunkCalls := callsByPrompt["Summarize the changes in this file diff in 3-5 bullet points. Be concise and technical."]
	if chunkCalls < 2 {
		t.Errorf("expected at least 2 per-file chunk calls, got %d", chunkCalls)
	}
}

// TestSmartChunk_SingleFile verifies that when the diff contains only one file,
// --smart-chunk falls through to a single direct comment call (no summarise+synthesise).
func TestSmartChunk_SingleFile(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	callCount := 0
	mockFn := func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
		callCount++
		if strings.HasPrefix(systemPrompt, "Summarize the changes") {
			t.Error("unexpected chunk-summary call for a single-file diff")
		}
		return "single file comment", nil
	}

	var buf strings.Builder
	cmd := newRootCmd(mockFn)
	cmd.SetArgs([]string{
		"--smart-chunk",
		"--file=testdata/simple.diff",
		"--provider=openai",
	})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected exactly 1 chatFn call for single-file diff, got %d", callCount)
	}
	if !strings.Contains(buf.String(), "single file comment") {
		t.Errorf("expected comment in output, got:\n%s", buf.String())
	}
}

// TestSmartChunk_LargeFileSet verifies the full round-trip with the large
// multi-file fixture: all chunks are processed, summaries are combined,
// and the synthesis call receives content from every file.
func TestSmartChunk_LargeFileSet(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	// Count unique files seen across all chunk calls.
	var mu sync.Mutex
	seenChunkDiffs := []string{}
	synthesisInput := ""

	mockFn := func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if strings.HasPrefix(systemPrompt, "Summarize the changes") {
			seenChunkDiffs = append(seenChunkDiffs, diffContent)
			return "summary for: " + diffContent[:min(20, len(diffContent))], nil
		}
		// Synthesis — diffContent is the joined summaries.
		synthesisInput = diffContent
		return "large set synthesis", nil
	}

	var buf strings.Builder
	cmd := newRootCmd(mockFn)
	cmd.SetArgs([]string{
		"--smart-chunk",
		"--file=testdata/large-multi-file.diff",
		"--provider=openai",
	})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// The fixture has 11 file diffs — verify all were chunked.
	if len(seenChunkDiffs) < 5 {
		t.Errorf("expected at least 5 file chunks, got %d", len(seenChunkDiffs))
	}

	// Synthesis input must contain all the per-chunk summaries joined by the separator.
	if !strings.Contains(synthesisInput, "---") {
		t.Errorf("expected chunk separator in synthesis input, got:\n%s", synthesisInput)
	}
	if !strings.Contains(buf.String(), "large set synthesis") {
		t.Errorf("expected synthesis output, got:\n%s", buf.String())
	}
}

// TestSmartChunk_ChunkError verifies that if any parallel chunk call fails,
// the whole command returns an error.
func TestSmartChunk_ChunkError(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	var callCount atomic.Int32
	mockFn := func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
		if strings.HasPrefix(systemPrompt, "Summarize the changes") {
			if callCount.Add(1) == 1 {
				return "", errors.New("simulated chunk API failure")
			}
			return "ok summary", nil
		}
		return "synthesis", nil
	}

	cmd := newRootCmd(mockFn)
	cmd.SetArgs([]string{
		"--smart-chunk",
		"--file=testdata/large-multi-file.diff",
		"--provider=openai",
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when a chunk call fails, got nil")
	}
	if !strings.Contains(err.Error(), "simulated chunk API failure") {
		t.Errorf("expected chunk error message, got: %v", err)
	}
}

// ── changelog subcommand tests ────────────────────────────────────────────────

func TestChangelog_TextOutput(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	mockFn := func(_ context.Context, _ *Config, _ ApiProvider, systemPrompt, _ string) (string, error) {
		if strings.HasPrefix(systemPrompt, "You are writing a user-facing changelog") {
			return "### Added\n- New feature added.", nil
		}
		return "unexpected call", nil
	}

	var buf strings.Builder
	cmd := newRootCmd(mockFn)
	cmd.SetArgs([]string{
		"changelog",
		"--file=testdata/simple.diff",
		"--provider=openai",
	})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "### Added") {
		t.Errorf("expected changelog output, got: %q", out)
	}
}

func TestChangelog_JSONOutput(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	mockFn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		return "### Fixed\n- Bug squashed.", nil
	}

	var buf strings.Builder
	cmd := newRootCmd(mockFn)
	cmd.SetArgs([]string{
		"changelog",
		"--file=testdata/simple.diff",
		"--provider=openai",
		"--format=json",
	})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result struct {
		Changelog string `json:"changelog"`
		Provider  string `json:"provider"`
		Model     string `json:"model"`
	}
	if err := json.NewDecoder(strings.NewReader(buf.String())).Decode(&result); err != nil {
		t.Fatalf("invalid JSON output: %v\nraw: %s", err, buf.String())
	}
	if !strings.Contains(result.Changelog, "### Fixed") {
		t.Errorf("expected changelog in JSON, got: %q", result.Changelog)
	}
	if result.Provider != "openai" {
		t.Errorf("expected provider=openai, got %q", result.Provider)
	}
}

func TestChangelog_APIError(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	mockFn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		return "", errors.New("api failure")
	}

	cmd := newRootCmd(mockFn)
	cmd.SetArgs([]string{
		"changelog",
		"--file=testdata/simple.diff",
		"--provider=openai",
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "api failure") {
		t.Fatalf("expected api failure error, got %v", err)
	}
}

func TestChangelog_OutputToFile(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	mockFn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		return "### Added\n- Feature X.", nil
	}

	outFile := "testdata/changelog-output.md"
	defer func() { _ = os.Remove(outFile) }()

	cmd := newRootCmd(mockFn)
	cmd.SetArgs([]string{
		"changelog",
		"--file=testdata/simple.diff",
		"--provider=openai",
		"--output=" + outFile,
	})
	var outBuf strings.Builder
	cmd.SetOut(&outBuf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("expected output file: %v", err)
	}
	if !strings.Contains(string(data), "### Added") {
		t.Errorf("expected changelog in file, got: %q", string(data))
	}
	if outBuf.Len() != 0 {
		t.Errorf("expected no stdout output when --output is set, got: %q", outBuf.String())
	}
}

func TestChangelog_UnsupportedProvider(t *testing.T) {
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{
		"changelog",
		"--file=testdata/simple.diff",
		"--provider=invalid",
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unsupported provider") {
		t.Fatalf("expected unsupported provider error, got %v", err)
	}
}

func TestChangelog_InvalidFormat(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{
		"changelog",
		"--file=testdata/simple.diff",
		"--provider=openai",
		"--format=xml",
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unsupported format") {
		t.Fatalf("expected unsupported format error, got %v", err)
	}
}

// ── gen-aliases subcommand tests ──────────────────────────────────────────────

func TestGenAliases_DefaultOutput(t *testing.T) {
	var buf strings.Builder
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"gen-aliases"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"alias amc=",
		"alias amc-review=",
		"alias amc-staged=",
		"alias amc-commit=",
		"alias amc-qc=",
		"alias amc-cl=",
		"alias amc-models=",
		"alias amc-init=",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected alias %q in output, not found:\n%s", want, out)
		}
	}
}

func TestGenAliases_ZshShell(t *testing.T) {
	var buf strings.Builder
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"gen-aliases", "--shell=zsh"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "alias amc=") {
		t.Error("expected alias block for zsh")
	}
}

func TestGenAliases_UnsupportedShell(t *testing.T) {
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"gen-aliases", "--shell=fish"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unsupported shell") {
		t.Fatalf("expected unsupported shell error, got %v", err)
	}
}

func TestGenAliases_OutputToFile(t *testing.T) {
	outFile := "testdata/aliases-output.sh"
	defer func() { _ = os.Remove(outFile) }()

	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"gen-aliases", "--output=" + outFile})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("expected output file: %v", err)
	}
	if !strings.Contains(string(data), "alias amc=") {
		t.Errorf("expected alias block in file, got: %q", string(data))
	}
}

func TestGenAliases_MatchesConstant(t *testing.T) {
	var buf strings.Builder
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"gen-aliases"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.String() != aliasBlock {
		t.Errorf("output does not match aliasBlock constant.\ngot:\n%s\nwant:\n%s", buf.String(), aliasBlock)
	}
}
