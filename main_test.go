package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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
	t.Setenv("OPENAI_API_KEY", "dummy")
	alwaysOkFn := func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
		return "ok", nil
	}
	cmd := newRootCmd(alwaysOkFn)
	// No --file flag, so it uses getGitDiff (we're in a git repo)
	cmd.SetArgs([]string{"--provider=openai"})

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
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--clipboard", "--file=testdata/diff.txt", "--provider=openai"})

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	// Clipboard may fail in headless CI environments; that's a warning, not an error
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
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
	if !strings.Contains(buf.String(), "Add mocked feature") {
		t.Error("expected title in output")
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
	if result["comment"] != "mocked comment" {
		t.Errorf("expected comment 'mocked comment', got %q", result["comment"])
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
	if result["comment"] != "mocked comment" {
		t.Errorf("expected comment 'mocked comment', got %q", result["comment"])
	}
	if result["provider"] == "" {
		t.Error("expected non-empty provider in JSON output")
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
