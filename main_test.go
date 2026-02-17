package main

import (
	"context"
	"errors"
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
	cmd.SetArgs([]string{"--debug", "--file=testdata/diff.txt", "--provider=openai"})

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
