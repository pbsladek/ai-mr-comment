package main

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func dummyChatFn(cfg *Config, provider ApiProvider, prompt, diff string) (string, error) {
	if strings.Contains(diff, "fail") {
		return "", errors.New("forced error")
	}
	return "mocked comment", nil
}

func TestWithSpinner_Success(t *testing.T) {
	called := false
	err := withSpinner("testing spinner", func() error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !called {
		t.Fatalf("expected function to be called")
	}
}

func TestWithSpinner_Error(t *testing.T) {
	err := withSpinner("testing spinner error", func() error {
		return errors.New("expected error")
	})
	if err == nil || err.Error() != "expected error" {
		t.Fatalf("expected 'expected error', got %v", err)
	}
}

func TestNewRootCmd_DebugFlag(t *testing.T) {
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--debug", "--file=testdata/diff.txt", "--provider=openai"})

	origStdout := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	err := cmd.Execute()

	w.Close()
	os.Stdout = origStdout

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestNewRootCmd_UnsupportedProvider(t *testing.T) {
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--file=testdata/diff.txt", "--provider=invalid"})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unsupported provider") {
		t.Fatalf("expected unsupported provider error, got %v", err)
	}
}

func TestNewRootCmd_ChatFnError(t *testing.T) {
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--file=testdata/diff.txt", "--provider=openai"})

	_ = os.WriteFile("testdata/fail.txt", []byte("this should fail"), 0644)
	defer os.Remove("testdata/fail.txt")

	cmd.SetArgs([]string{"--file=testdata/fail.txt", "--provider=openai"})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "forced error") {
		t.Fatalf("expected chat error, got %v", err)
	}
}

func TestNewRootCmd_OutputToFile(t *testing.T) {
	outputFile := "testdata/output.txt"
	defer os.Remove(outputFile)

	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--file=testdata/diff.txt", "--provider=openai", "--output=" + outputFile})

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
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--file=testdata/doesnotexist.diff", "--provider=openai"})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "no such file or directory") {
		t.Fatalf("expected file not found error, got %v", err)
	}
}

func TestNewRootCmd_EmptyDiff(t *testing.T) {
	cmd := newRootCmd(func(cfg *Config, provider ApiProvider, prompt, diff string) (string, error) {
		return "", nil
	})
	cmd.SetArgs([]string{"--file=testdata/diff.txt", "--provider=openai"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestNewRootCmd_DebugOnly(t *testing.T) {
	cmd := newRootCmd(func(cfg *Config, provider ApiProvider, prompt, diff string) (string, error) {
		t.Fatalf("chatFn should not be called in debug mode")
		return "", nil
	})
	cmd.SetArgs([]string{"--file=testdata/diff.txt", "--provider=openai", "--debug"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}
