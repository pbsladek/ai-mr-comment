package main

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

// ── resolveSystemPrompt unit tests ────────────────────────────────────────────

func TestResolveSystemPrompt_Empty(t *testing.T) {
	got, err := resolveSystemPrompt("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestResolveSystemPrompt_Inline(t *testing.T) {
	got, err := resolveSystemPrompt("Focus only on security issues.")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Focus only on security issues." {
		t.Errorf("unexpected result: %q", got)
	}
}

func TestResolveSystemPrompt_AtFile(t *testing.T) {
	f, err := os.CreateTemp("", "prompt-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	_, _ = f.WriteString("  Custom prompt from file.  ")
	_ = f.Close()

	got, err := resolveSystemPrompt("@" + f.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Custom prompt from file." {
		t.Errorf("expected trimmed file content, got %q", got)
	}
}

func TestResolveSystemPrompt_AtFileMissing(t *testing.T) {
	_, err := resolveSystemPrompt("@/does/not/exist.txt")
	if err == nil || !strings.Contains(err.Error(), "cannot read file") {
		t.Fatalf("expected file-read error, got %v", err)
	}
}

// ── --system-prompt flag on main command ─────────────────────────────────────

func TestSystemPrompt_InlineOverride(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	var capturedPrompt string
	mockFn := func(_ context.Context, _ *Config, _ ApiProvider, systemPrompt, _ string) (string, error) {
		capturedPrompt = systemPrompt
		return "mocked comment", nil
	}

	var buf strings.Builder
	cmd := newRootCmd(mockFn)
	cmd.SetArgs([]string{
		"--file=testdata/simple.diff",
		"--provider=openai",
		"--system-prompt=Focus only on security issues.",
	})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedPrompt != "Focus only on security issues." {
		t.Errorf("system prompt not applied; got: %q", capturedPrompt)
	}
}

func TestSystemPrompt_AtFileOverride(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	f, err := os.CreateTemp("", "prompt-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	_, _ = f.WriteString("Prompt from file.")
	_ = f.Close()

	var capturedPrompt string
	mockFn := func(_ context.Context, _ *Config, _ ApiProvider, systemPrompt, _ string) (string, error) {
		capturedPrompt = systemPrompt
		return "mocked comment", nil
	}

	var buf strings.Builder
	cmd := newRootCmd(mockFn)
	cmd.SetArgs([]string{
		"--file=testdata/simple.diff",
		"--provider=openai",
		"--system-prompt=@" + f.Name(),
	})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedPrompt != "Prompt from file." {
		t.Errorf("expected file prompt, got: %q", capturedPrompt)
	}
}

func TestSystemPrompt_MutuallyExclusiveWithTemplate(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{
		"--file=testdata/simple.diff",
		"--provider=openai",
		"--system-prompt=hello",
		"--template=technical",
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive error, got %v", err)
	}
}

func TestSystemPrompt_AtFileMissing_MainCmd(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{
		"--file=testdata/simple.diff",
		"--provider=openai",
		"--system-prompt=@/no/such/file.txt",
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "cannot read file") {
		t.Fatalf("expected file-read error, got %v", err)
	}
}

// ── --system-prompt flag on changelog subcommand ─────────────────────────────

func TestSystemPrompt_Changelog_InlineOverride(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	var capturedPrompt string
	mockFn := func(_ context.Context, _ *Config, _ ApiProvider, systemPrompt, _ string) (string, error) {
		capturedPrompt = systemPrompt
		return "### Added\n- Something.", nil
	}

	var buf strings.Builder
	cmd := newRootCmd(mockFn)
	cmd.SetArgs([]string{
		"changelog",
		"--file=testdata/simple.diff",
		"--provider=openai",
		"--system-prompt=List only breaking changes.",
	})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedPrompt != "List only breaking changes." {
		t.Errorf("system prompt not applied to changelog; got: %q", capturedPrompt)
	}
}
