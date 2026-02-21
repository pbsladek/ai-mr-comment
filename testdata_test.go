package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

// TestCmd_EmptyDiff_ReturnsError verifies that --file=testdata/empty.diff causes
// the command to return a "no diff found" error without calling the AI.
func TestCmd_EmptyDiff_ReturnsError(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	aiCalled := false
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		aiCalled = true
		return "should not be called", nil
	}

	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--file=testdata/empty.diff", "--provider=openai"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for empty diff, got nil")
	}
	if !strings.Contains(err.Error(), "no diff found") {
		t.Errorf("expected 'no diff found' error, got: %v", err)
	}
	if aiCalled {
		t.Error("expected AI not to be called for empty diff")
	}
}

// TestCmd_BinaryFilesDiff_CallsAI verifies that a binary-files diff is
// non-empty and reaches the AI.
func TestCmd_BinaryFilesDiff_CallsAI(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	aiCalled := false
	var capturedDiff string
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, diffContent string) (string, error) {
		aiCalled = true
		capturedDiff = diffContent
		return "binary files reviewed", nil
	}

	var buf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--file=testdata/binary-files.diff", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !aiCalled {
		t.Error("expected AI to be called for binary-files.diff")
	}
	if !strings.Contains(capturedDiff, "Binary files") {
		t.Errorf("expected 'Binary files' in diff sent to AI, got: %q", capturedDiff[:min(200, len(capturedDiff))])
	}
	if !strings.Contains(buf.String(), "binary files reviewed") {
		t.Errorf("expected AI response in output, got: %s", buf.String())
	}
}

// TestCmd_MergeConflictMarkersDiff verifies that a diff resolving merge
// conflict markers is processed without errors.
func TestCmd_MergeConflictMarkersDiff(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		return "conflict resolved review", nil
	}

	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--file=testdata/merge-conflict-markers.diff", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error for merge-conflict-markers.diff: %v", err)
	}
}

// TestCmd_WhitespaceOnlyDiff verifies that a diff with only whitespace changes
// is treated as a valid non-empty diff and the AI is called.
func TestCmd_WhitespaceOnlyDiff(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	aiCalled := false
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		aiCalled = true
		return "whitespace changes reviewed", nil
	}

	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--file=testdata/whitespace-only.diff", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error for whitespace-only.diff: %v", err)
	}
	if !aiCalled {
		t.Error("expected AI to be called for whitespace-only.diff")
	}
}

// TestCmd_SubmoduleChangesDiff verifies that a submodule update diff is
// processed without errors.
func TestCmd_SubmoduleChangesDiff(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		return "submodule update reviewed", nil
	}

	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--file=testdata/submodule-changes.diff", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error for submodule-changes.diff: %v", err)
	}
}

// TestCmd_TruncationTriggerDiff_SmartChunk verifies that --smart-chunk handles
// a very large multi-file diff by making multiple chatFn calls.
func TestCmd_TruncationTriggerDiff_SmartChunk(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	callCount := 0
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		callCount++
		return "chunk summary", nil
	}

	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--smart-chunk", "--file=testdata/truncation-trigger.diff", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error for truncation-trigger.diff with --smart-chunk: %v", err)
	}
	// Expect at least: N chunk-summary calls + 1 synthesis call.
	if callCount < 2 {
		t.Errorf("expected multiple chatFn calls for large multi-file diff, got %d", callCount)
	}
}

