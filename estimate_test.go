package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

// TestEstimateFlag_ShowsEstimationAndProceeds verifies that --estimate --yes
// displays cost info and still makes the API call.
func TestEstimateFlag_ShowsEstimationAndProceeds(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	aiCalled := false
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		aiCalled = true
		return "mocked comment", nil
	}

	var outBuf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--estimate", "--yes", "--file=testdata/simple.diff", "--provider=openai"})
	cmd.SetOut(&outBuf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !aiCalled {
		t.Error("expected AI to be called when user confirms with --yes")
	}
	out := outBuf.String()
	if !strings.Contains(out, "Token & Cost Estimation:") {
		t.Errorf("expected cost estimation in output, got:\n%s", out)
	}
	if !strings.Contains(out, "mocked comment") {
		t.Errorf("expected AI response in output after confirmation, got:\n%s", out)
	}
}

// TestEstimateFlag_NoCallWhenDeclined verifies that in a non-interactive
// environment --estimate without --yes auto-declines and the API is not called.
func TestEstimateFlag_NoCallWhenDeclined(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	aiCalled := false
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		aiCalled = true
		return "should not appear", nil
	}

	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--estimate", "--file=testdata/simple.diff", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if aiCalled {
		t.Error("expected AI NOT to be called when estimate was declined (non-interactive)")
	}
}

// TestEstimateFlag_DoesNotBreakDebug verifies that --debug still exits early
// and skips the API call even when --estimate is also set.
func TestEstimateFlag_DoesNotBreakDebug(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	aiCalled := false
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		aiCalled = true
		return "should not appear", nil
	}

	var outBuf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--debug", "--estimate", "--file=testdata/simple.diff", "--provider=openai"})
	cmd.SetOut(&outBuf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if aiCalled {
		t.Error("expected AI NOT to be called when --debug is set (even with --estimate)")
	}
	if !strings.Contains(outBuf.String(), "Token & Cost Estimation:") {
		t.Errorf("expected debug cost output, got:\n%s", outBuf.String())
	}
}

// TestEstimateFlag_Changelog verifies that the changelog subcommand also
// supports --estimate --yes and calls the AI after confirmation.
func TestEstimateFlag_Changelog(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	aiCalled := false
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		aiCalled = true
		return "### Added\n- Feature.", nil
	}

	var outBuf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"changelog", "--estimate", "--yes", "--file=testdata/simple.diff", "--provider=openai"})
	cmd.SetOut(&outBuf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !aiCalled {
		t.Error("expected AI to be called after --yes confirmation for changelog")
	}
}

// TestPromptConfirm_AutoYes verifies that autoYes=true skips the prompt and
// returns true without writing any output.
func TestPromptConfirm_AutoYes(t *testing.T) {
	var w strings.Builder
	result := promptConfirm(&w, strings.NewReader(""), true)
	if !result {
		t.Error("expected promptConfirm to return true with autoYes=true")
	}
	if w.Len() > 0 {
		t.Errorf("expected no output with autoYes=true, got: %s", w.String())
	}
}

// TestPromptConfirm_NonInteractiveDeclines verifies that a non-*os.File reader
// (simulating piped/test stdin) causes auto-decline.
func TestPromptConfirm_NonInteractiveDeclines(t *testing.T) {
	var w strings.Builder
	r := strings.NewReader("y\n")
	result := promptConfirm(&w, r, false)
	if result {
		t.Error("expected promptConfirm to return false for non-TTY reader")
	}
	if !strings.Contains(w.String(), "Non-interactive") {
		t.Errorf("expected non-interactive message, got: %s", w.String())
	}
}

// TestShowCostEstimate_ContainsExpectedFields verifies that showCostEstimate
// writes all expected output fields.
func TestShowCostEstimate_ContainsExpectedFields(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	cfg, _ := loadConfig()
	cfg.Provider = OpenAI

	var buf strings.Builder
	showCostEstimate(context.Background(), cfg, "system prompt text", "diff content text", &buf)

	out := buf.String()
	for _, want := range []string{
		"Token & Cost Estimation:",
		"- Model:",
		"- Diff lines:",
		"- Estimated Input Tokens:",
		"- Estimated Input Cost:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got:\n%s", want, out)
		}
	}
}
