package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeFakeBinary writes a shell script to a temp dir that prints stdout,
// optionally writes to stderr, and exits with exitCode.
// The path is unique per test via t.TempDir().
func makeFakeBinary(t *testing.T, stdout, stderr string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fakecli")
	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	if stderr != "" {
		fmt.Fprintf(&sb, "echo %q >&2\n", stderr)
	}
	if stdout != "" {
		fmt.Fprintf(&sb, "echo %q\n", stdout)
	}
	fmt.Fprintf(&sb, "exit %d\n", exitCode)
	if err := os.WriteFile(path, []byte(sb.String()), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

// --- cliPrompt ---

func TestCliPrompt_CombinesInputs(t *testing.T) {
	got := cliPrompt("system", "diff")
	if !strings.Contains(got, "system") || !strings.Contains(got, "diff") {
		t.Errorf("expected both inputs in result, got %q", got)
	}
}

func TestCliPrompt_StripsNullBytes(t *testing.T) {
	got := cliPrompt("sys\x00tem", "di\x00ff")
	if strings.Contains(got, "\x00") {
		t.Error("expected null bytes to be stripped")
	}
	if !strings.Contains(got, "system") || !strings.Contains(got, "diff") {
		t.Errorf("expected content preserved after stripping nulls, got %q", got)
	}
}

// --- cliExecError ---

func TestCliExecError_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := cliExecError(ctx, "mybin", errors.New("exit status 1"), "")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled wrapped in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "mybin") {
		t.Errorf("expected binary name in error, got %q", err.Error())
	}
}

func TestCliExecError_WithStderr(t *testing.T) {
	err := cliExecError(context.Background(), "mybin", errors.New("exit status 1"), "  auth failed  ")
	if !strings.Contains(err.Error(), "auth failed") {
		t.Errorf("expected stderr message in error, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "mybin") {
		t.Errorf("expected binary name in error, got %q", err.Error())
	}
}

func TestCliExecError_WithoutStderr(t *testing.T) {
	cause := errors.New("exit status 2")
	err := cliExecError(context.Background(), "mybin", cause, "")
	if !errors.Is(err, cause) {
		t.Errorf("expected original error wrapped, got %v", err)
	}
}

// --- execCLI ---

func TestExecCLI_Success(t *testing.T) {
	binary := makeFakeBinary(t, "hello response", "", 0)
	result, err := execCLI(context.Background(), binary, []string{"--arg"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello response" {
		t.Errorf("expected 'hello response', got %q", result)
	}
}

func TestExecCLI_EmptyOutput(t *testing.T) {
	binary := makeFakeBinary(t, "", "", 0)
	_, err := execCLI(context.Background(), binary, nil)
	if err == nil {
		t.Fatal("expected error for empty output")
	}
	if !strings.Contains(err.Error(), "empty output") {
		t.Errorf("expected 'empty output' in error, got %q", err.Error())
	}
}

func TestExecCLI_NonZeroExitWithStderr(t *testing.T) {
	binary := makeFakeBinary(t, "", "auth failed", 1)
	_, err := execCLI(context.Background(), binary, nil)
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	if !strings.Contains(err.Error(), "auth failed") {
		t.Errorf("expected stderr message in error, got %q", err.Error())
	}
}

func TestExecCLI_NonZeroExitWithoutStderr(t *testing.T) {
	binary := makeFakeBinary(t, "", "", 1)
	_, err := execCLI(context.Background(), binary, nil)
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
}

func TestExecCLI_BinaryNotFound(t *testing.T) {
	_, err := execCLI(context.Background(), "/nonexistent/binary", nil)
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

// --- streamExecCLI ---

func TestStreamExecCLI_Success(t *testing.T) {
	binary := makeFakeBinary(t, "streamed output", "", 0)
	var buf strings.Builder
	result, err := streamExecCLI(context.Background(), binary, nil, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "streamed output" {
		t.Errorf("expected 'streamed output', got %q", result)
	}
	if !strings.Contains(buf.String(), "streamed output") {
		t.Errorf("expected writer to receive output, got %q", buf.String())
	}
}

func TestStreamExecCLI_EmptyOutput(t *testing.T) {
	binary := makeFakeBinary(t, "", "", 0)
	_, err := streamExecCLI(context.Background(), binary, nil, io.Discard)
	if err == nil {
		t.Fatal("expected error for empty output")
	}
	if !strings.Contains(err.Error(), "empty output") {
		t.Errorf("expected 'empty output' in error, got %q", err.Error())
	}
}

func TestStreamExecCLI_NonZeroExit(t *testing.T) {
	binary := makeFakeBinary(t, "", "stream error", 1)
	_, err := streamExecCLI(context.Background(), binary, nil, io.Discard)
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	if !strings.Contains(err.Error(), "stream error") {
		t.Errorf("expected stderr in error, got %q", err.Error())
	}
}

// --- arg builders ---

func TestClaudeCLIArgs_NoModel(t *testing.T) {
	cfg := &Config{}
	args := claudeCLIArgs(cfg, "the-prompt")
	assertArgOrder(t, args, "--output-format", "text", "-p", "the-prompt")
	if containsFlag(args, "--model") {
		t.Error("expected no --model flag when model is empty")
	}
}

func TestClaudeCLIArgs_WithModel(t *testing.T) {
	cfg := &Config{ClaudeCLIModel: "claude-sonnet-4-6"}
	args := claudeCLIArgs(cfg, "the-prompt")
	assertArgOrder(t, args, "--model", "claude-sonnet-4-6", "-p", "the-prompt")
	assertFlagBeforePrompt(t, args, "--output-format", "-p")
	assertFlagBeforePrompt(t, args, "--model", "-p")
}

func TestGeminiCLIArgs_NoModel(t *testing.T) {
	cfg := &Config{}
	args := geminiCLIArgs(cfg, "the-prompt")
	assertArgOrder(t, args, "-p", "the-prompt")
	if containsFlag(args, "--model") {
		t.Error("expected no --model flag when model is empty")
	}
}

func TestGeminiCLIArgs_WithModel(t *testing.T) {
	cfg := &Config{GeminiCLIModel: "gemini-2.5-flash"}
	args := geminiCLIArgs(cfg, "the-prompt")
	assertArgOrder(t, args, "--model", "gemini-2.5-flash", "-p", "the-prompt")
	assertFlagBeforePrompt(t, args, "--model", "-p")
}

func TestCodexCLIArgs_NoModel(t *testing.T) {
	cfg := &Config{}
	args := codexCLIArgs(cfg, "the-prompt")
	assertArgOrder(t, args, "exec", "the-prompt")
	if containsFlag(args, "-m") {
		t.Error("expected no -m flag when model is empty")
	}
}

func TestCodexCLIArgs_WithModel(t *testing.T) {
	cfg := &Config{CodexCLIModel: "o4-mini"}
	args := codexCLIArgs(cfg, "the-prompt")
	assertArgOrder(t, args, "exec", "-m", "o4-mini", "the-prompt")
}

// --- findClaudeBinary ---

func TestFindClaudeBinary_ExplicitPath(t *testing.T) {
	binary := makeFakeBinary(t, "", "", 0)
	cfg := &Config{ClaudeCLIPath: binary}
	got, err := findClaudeBinary(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != binary {
		t.Errorf("expected explicit path, got %q", got)
	}
}

func TestFindClaudeBinary_ExplicitPathMissing(t *testing.T) {
	cfg := &Config{ClaudeCLIPath: "/nonexistent/claude"}
	_, err := findClaudeBinary(cfg)
	if err == nil {
		t.Fatal("expected error for missing explicit path")
	}
	if !strings.Contains(err.Error(), "/nonexistent/claude") {
		t.Errorf("expected path in error message, got %q", err.Error())
	}
}

func TestFindGeminiCLIBinary_ExplicitPath(t *testing.T) {
	binary := makeFakeBinary(t, "", "", 0)
	cfg := &Config{GeminiCLIPath: binary}
	got, err := findGeminiCLIBinary(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != binary {
		t.Errorf("expected explicit path, got %q", got)
	}
}

func TestFindGeminiCLIBinary_ExplicitPathMissing(t *testing.T) {
	cfg := &Config{GeminiCLIPath: "/nonexistent/gemini"}
	_, err := findGeminiCLIBinary(cfg)
	if err == nil {
		t.Fatal("expected error for missing explicit path")
	}
	if !strings.Contains(err.Error(), "/nonexistent/gemini") {
		t.Errorf("expected path in error message, got %q", err.Error())
	}
}

func TestFindGeminiCLIBinary_NotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	cfg := &Config{}
	_, err := findGeminiCLIBinary(cfg)
	if err == nil {
		t.Fatal("expected error when gemini not found")
	}
	if !strings.Contains(err.Error(), "gemini CLI not found") {
		t.Errorf("expected install hint in error, got %q", err.Error())
	}
}

func TestFindCodexBinary_ExplicitPath(t *testing.T) {
	binary := makeFakeBinary(t, "", "", 0)
	cfg := &Config{CodexCLIPath: binary}
	got, err := findCodexBinary(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != binary {
		t.Errorf("expected explicit path, got %q", got)
	}
}

func TestFindCodexBinary_ExplicitPathMissing(t *testing.T) {
	cfg := &Config{CodexCLIPath: "/nonexistent/codex"}
	_, err := findCodexBinary(cfg)
	if err == nil {
		t.Fatal("expected error for missing explicit path")
	}
	if !strings.Contains(err.Error(), "/nonexistent/codex") {
		t.Errorf("expected path in error message, got %q", err.Error())
	}
}

func TestFindCodexBinary_NotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	cfg := &Config{}
	_, err := findCodexBinary(cfg)
	if err == nil {
		t.Fatal("expected error when codex not found")
	}
	if !strings.Contains(err.Error(), "codex CLI not found") {
		t.Errorf("expected install hint in error, got %q", err.Error())
	}
}

func TestFindClaudeBinary_HomeDir(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude", "local")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatal(err)
	}
	claudePath := filepath.Join(claudeDir, "claude")
	if err := os.WriteFile(claudePath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	cfg := &Config{}
	got, err := findClaudeBinary(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != claudePath {
		t.Errorf("expected home-dir path %q, got %q", claudePath, got)
	}
}

func TestFindClaudeBinary_PathLookup(t *testing.T) {
	dir := t.TempDir()
	claudePath := filepath.Join(dir, "claude")
	if err := os.WriteFile(claudePath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	// Use a home with no ~/.claude/local/claude so it falls through to PATH.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cfg := &Config{}
	got, err := findClaudeBinary(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != claudePath {
		t.Errorf("expected PATH binary %q, got %q", claudePath, got)
	}
}

func TestFindClaudeBinary_NotFound(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir()) // empty dir, no claude binary

	cfg := &Config{}
	_, err := findClaudeBinary(cfg)
	if err == nil {
		t.Fatal("expected error when claude not found")
	}
	if !strings.Contains(err.Error(), "claude CLI not found") {
		t.Errorf("expected install hint in error, got %q", err.Error())
	}
}

// --- callClaudeCLI / callGeminiCLI / callCodexCLI ---

func TestCallClaudeCLI_Success(t *testing.T) {
	binary := makeFakeBinary(t, "claude review output", "", 0)
	cfg := &Config{ClaudeCLIPath: binary, ClaudeCLIModel: "claude-sonnet-4-6"}
	result, err := callClaudeCLI(context.Background(), cfg, "system", "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "claude review output" {
		t.Errorf("expected 'claude review output', got %q", result)
	}
}

func TestCallClaudeCLI_BinaryFails(t *testing.T) {
	binary := makeFakeBinary(t, "", "claude error", 1)
	cfg := &Config{ClaudeCLIPath: binary}
	_, err := callClaudeCLI(context.Background(), cfg, "system", "diff")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "claude error") {
		t.Errorf("expected stderr in error, got %q", err.Error())
	}
}

func TestCallGeminiCLI_Success(t *testing.T) {
	binary := makeFakeBinary(t, "gemini review output", "", 0)
	cfg := &Config{GeminiCLIPath: binary, GeminiCLIModel: "gemini-2.5-flash"}
	result, err := callGeminiCLI(context.Background(), cfg, "system", "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "gemini review output" {
		t.Errorf("expected 'gemini review output', got %q", result)
	}
}

func TestCallGeminiCLI_BinaryFails(t *testing.T) {
	binary := makeFakeBinary(t, "", "gemini error", 1)
	cfg := &Config{GeminiCLIPath: binary}
	_, err := callGeminiCLI(context.Background(), cfg, "system", "diff")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCallCodexCLI_Success(t *testing.T) {
	binary := makeFakeBinary(t, "codex review output", "", 0)
	cfg := &Config{CodexCLIPath: binary}
	result, err := callCodexCLI(context.Background(), cfg, "system", "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "codex review output" {
		t.Errorf("expected 'codex review output', got %q", result)
	}
}

func TestCallCodexCLI_BinaryFails(t *testing.T) {
	binary := makeFakeBinary(t, "", "codex error", 1)
	cfg := &Config{CodexCLIPath: binary}
	_, err := callCodexCLI(context.Background(), cfg, "system", "diff")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- chatCompletions dispatch for CLI providers ---

func TestChatCompletions_ClaudeCLI(t *testing.T) {
	binary := makeFakeBinary(t, "claude via chat", "", 0)
	cfg := &Config{Provider: ClaudeCLI, ClaudeCLIPath: binary}
	result, err := chatCompletions(context.Background(), cfg, ClaudeCLI, "sys", "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "claude via chat" {
		t.Errorf("expected 'claude via chat', got %q", result)
	}
}

func TestChatCompletions_GeminiCLI(t *testing.T) {
	binary := makeFakeBinary(t, "gemini via chat", "", 0)
	cfg := &Config{Provider: GeminiCLI, GeminiCLIPath: binary}
	result, err := chatCompletions(context.Background(), cfg, GeminiCLI, "sys", "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "gemini via chat" {
		t.Errorf("expected 'gemini via chat', got %q", result)
	}
}

func TestChatCompletions_CodexCLI(t *testing.T) {
	binary := makeFakeBinary(t, "codex via chat", "", 0)
	cfg := &Config{Provider: CodexCLI, CodexCLIPath: binary}
	result, err := chatCompletions(context.Background(), cfg, CodexCLI, "sys", "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "codex via chat" {
		t.Errorf("expected 'codex via chat', got %q", result)
	}
}

// --- streamToWriter dispatch for CLI providers ---

func TestStreamToWriter_ClaudeCLI(t *testing.T) {
	binary := makeFakeBinary(t, "claude stream", "", 0)
	cfg := &Config{Provider: ClaudeCLI, ClaudeCLIPath: binary}
	var buf strings.Builder
	result, err := streamToWriter(context.Background(), cfg, ClaudeCLI, "sys", "diff", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "claude stream" {
		t.Errorf("expected 'claude stream', got %q", result)
	}
}

func TestStreamToWriter_GeminiCLI(t *testing.T) {
	binary := makeFakeBinary(t, "gemini stream", "", 0)
	cfg := &Config{Provider: GeminiCLI, GeminiCLIPath: binary}
	result, err := streamToWriter(context.Background(), cfg, GeminiCLI, "sys", "diff", io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "gemini stream" {
		t.Errorf("expected 'gemini stream', got %q", result)
	}
}

func TestStreamToWriter_CodexCLI(t *testing.T) {
	binary := makeFakeBinary(t, "codex stream", "", 0)
	cfg := &Config{Provider: CodexCLI, CodexCLIPath: binary}
	result, err := streamToWriter(context.Background(), cfg, CodexCLI, "sys", "diff", io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "codex stream" {
		t.Errorf("expected 'codex stream', got %q", result)
	}
}

// --- validateAPIKey does not require keys for CLI providers ---

func TestValidateAPIKey_CLIProvidersNoKeyRequired(t *testing.T) {
	cfg := &Config{} // all API keys empty
	for _, provider := range []ApiProvider{ClaudeCLI, GeminiCLI, CodexCLI, Ollama} {
		if err := validateAPIKey(provider, cfg); err != nil {
			t.Errorf("provider %q: expected no error for empty keys, got %v", provider, err)
		}
	}
}

// --- helpers ---

// assertArgOrder checks that all of want appear in args in that relative order.
func assertArgOrder(t *testing.T, args []string, want ...string) {
	t.Helper()
	pos := 0
	for _, w := range want {
		found := false
		for pos < len(args) {
			if args[pos] == w {
				pos++
				found = true
				break
			}
			pos++
		}
		if !found {
			t.Errorf("arg %q not found in expected order in %v", w, args)
			return
		}
	}
}

// assertFlagBeforePrompt checks that flag appears before promptFlag in args.
func assertFlagBeforePrompt(t *testing.T, args []string, flag, promptFlag string) {
	t.Helper()
	fi, pi := -1, -1
	for i, a := range args {
		if a == flag && fi == -1 {
			fi = i
		}
		if a == promptFlag && pi == -1 {
			pi = i
		}
	}
	if fi == -1 {
		t.Errorf("flag %q not found in args %v", flag, args)
		return
	}
	if pi == -1 {
		t.Errorf("prompt flag %q not found in args %v", promptFlag, args)
		return
	}
	if fi >= pi {
		t.Errorf("expected %q before %q in args %v", flag, promptFlag, args)
	}
}

// containsFlag reports whether args contains the given flag string.
func containsFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}
