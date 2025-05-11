package main

import (
	_ "embed"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeChatCompletions is injected to avoid calling real APIs.
func fakeChatCompletions(cfg *Config, provider ApiProvider, prompt, diff string) (string, error) {
	return "mocked comment", nil
}

func TestRootCmd_WithFileFlag(t *testing.T) {
	tmpDir := t.TempDir()
	diffPath := filepath.Join(tmpDir, "diff.txt")

	err := os.WriteFile(diffPath, []byte("diff --git a/x b/x\n+line"), 0644)
	require.NoError(t, err)

	cmd := newRootCmd(fakeChatCompletions)
	cmd.SetArgs([]string{"--file=" + diffPath, "--provider=openai"})

	err = cmd.Execute()
	require.NoError(t, err)
}

func TestRootCmd_WithDebugFlag(t *testing.T) {
	tmpDir := t.TempDir()
	diffPath := filepath.Join(tmpDir, "debug.diff")

	err := os.WriteFile(diffPath, []byte("diff --git a/y b/y\n+debug line"), 0644)
	require.NoError(t, err)

	cmd := newRootCmd(fakeChatCompletions)
	cmd.SetArgs([]string{"--debug", "--file=" + diffPath})

	// Capture stdout temporarily if needed
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = cmd.Execute()
	require.NoError(t, err)

	w.Close()
	out, _ := io.ReadAll(r)
	os.Stdout = old

	require.Contains(t, string(out), "Token estimation:")
	require.Contains(t, string(out), "System prompt:")
	require.Contains(t, string(out), "Diff content:")
}

func TestRootCmd_WithOutputFlag(t *testing.T) {
	tmpDir := t.TempDir()
	diffPath := filepath.Join(tmpDir, "diff.txt")
	outputPath := filepath.Join(tmpDir, "output.txt")

	require.NoError(t, os.WriteFile(diffPath, []byte("diff --git a/z b/z\n+output line"), 0644))

	cmd := newRootCmd(fakeChatCompletions)
	cmd.SetArgs([]string{
		"--file=" + diffPath,
		"--output=" + outputPath,
		"--provider=openai",
	})

	err := cmd.Execute()
	require.NoError(t, err)

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	require.Equal(t, "mocked comment", strings.TrimSpace(string(data)))
}

func TestRootCmd_InvalidFile(t *testing.T) {
	cmd := newRootCmd(fakeChatCompletions)
	cmd.SetArgs([]string{"--file=doesnotexist.diff", "--provider=openai"})

	err := cmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "no such file or directory")
}

func TestRootCmd_UnsupportedProvider(t *testing.T) {
	tmpDir := t.TempDir()
	diffPath := filepath.Join(tmpDir, "diff.txt")
	_ = os.WriteFile(diffPath, []byte("fake diff"), 0644)

	cmd := newRootCmd(fakeChatCompletions)
	cmd.SetArgs([]string{"--file=" + diffPath, "--provider=unknown"})

	err := cmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported provider")
}

func TestWithSpinner(t *testing.T) {
	called := false

	err := withSpinner("Testing spinner", func() error {
		called = true
		time.Sleep(200 * time.Millisecond)
		return nil
	})

	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if !called {
		t.Fatal("expected function to be called")
	}
}

func TestWithSpinner_Error(t *testing.T) {
	testErr := errors.New("fail")

	err := withSpinner("Error case", func() error {
		time.Sleep(100 * time.Millisecond)
		return testErr
	})

	if err != testErr {
		t.Fatalf("expected %v, got %v", testErr, err)
	}
}
