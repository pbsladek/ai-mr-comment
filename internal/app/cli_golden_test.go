package app

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGoldenCommand(t *testing.T, chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error), args ...string) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "dummy")

	var out strings.Builder
	cmd := newRootCmd(chatFn)
	cmd.SetArgs(args)
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.Execute(); err != nil {
		t.Fatalf("command %v failed: %v\nstdout:\n%s", args, err, out.String())
	}
	return out.String()
}

func TestCLIGoldenVersionOutput(t *testing.T) {
	oldVersion, oldCommit, oldCommitFull := Version, Commit, CommitFull
	t.Cleanup(func() {
		Version, Commit, CommitFull = oldVersion, oldCommit, oldCommitFull
	})
	Version = "v1.2.3"
	Commit = "abc1234"
	CommitFull = "abc1234567890"

	out := runGoldenCommand(t, dummyChatFn, "--version")
	want := "version=v1.2.3\ncommit=abc1234\ncommit_full=abc1234567890\nrepo=https://github.com/pbsladek/ai-mr-comment\n"
	if out != want {
		t.Fatalf("version output mismatch\nwant:\n%q\ngot:\n%q", want, out)
	}
}

func TestCLIGoldenPrintRequestFromFile(t *testing.T) {
	out := runGoldenCommand(t, dummyChatFn, "--print-request", "--file="+testdataPath(t, "simple.diff"), "--provider=openai")

	var payload struct {
		Provider   string `json:"provider"`
		Model      string `json:"model"`
		Template   string `json:"template"`
		DiffSource string `json:"diff_source"`
		Diff       string `json:"diff"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid request JSON: %v\n%s", err, out)
	}
	if payload.Provider != "openai" {
		t.Fatalf("provider mismatch: %+v", payload)
	}
	if payload.Model == "" {
		t.Fatalf("expected model in request payload: %+v", payload)
	}
	if payload.Template == "" {
		t.Fatalf("expected template in request payload: %+v", payload)
	}
	if !strings.HasPrefix(payload.DiffSource, "file:") || !strings.Contains(payload.DiffSource, "simple.diff") {
		t.Fatalf("unexpected diff source: %+v", payload)
	}
	if !strings.Contains(payload.Diff, "+This is a simple but important project.") {
		t.Fatalf("expected fixture diff in request payload: %+v", payload)
	}
}

func TestCLIGoldenJSONReviewOutput(t *testing.T) {
	fn := func(_ context.Context, _ *Config, _ ApiProvider, systemPrompt, _ string) (string, error) {
		if systemPrompt == titlePrompt {
			return "feat: generated title", nil
		}
		return "Generated description", nil
	}

	out := runGoldenCommand(t, fn, "--format=json", "--file="+testdataPath(t, "simple.diff"), "--provider=openai")

	var payload struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Comment     string `json:"comment"`
		Provider    string `json:"provider"`
		Model       string `json:"model"`
		DiffSource  string `json:"diff_source"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid review JSON: %v\n%s", err, out)
	}
	if payload.Title != "feat: generated title" || payload.Description != "Generated description" || payload.Comment != payload.Description {
		t.Fatalf("unexpected review payload: %+v", payload)
	}
	if payload.Provider != "openai" || payload.Model == "" || !strings.Contains(payload.DiffSource, "simple.diff") {
		t.Fatalf("unexpected review metadata: %+v", payload)
	}
}

func TestCLIGoldenQuickCommitJSONDryRun(t *testing.T) {
	dir := initEmptyRepo(t)
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-C", dir, "add", "README.md"},
		{"-C", dir, "commit", "-m", "initial"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(readme, []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		return "fix: golden quick commit", nil
	}
	out := runGoldenCommand(t, fn, "quick-commit", "--dry-run", "--format=json", "--provider=openai")

	var payload struct {
		CommitMessage string `json:"commit_message"`
		Provider      string `json:"provider"`
		Model         string `json:"model"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid quick-commit JSON: %v\n%s", err, out)
	}
	if payload.CommitMessage != "fix: golden quick commit" || payload.Provider != "openai" || payload.Model == "" {
		t.Fatalf("unexpected quick-commit payload: %+v", payload)
	}
}
