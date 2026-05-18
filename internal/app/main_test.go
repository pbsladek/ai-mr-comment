package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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
	initEmptyRepo(t)

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

	var stdoutBuf strings.Builder
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--file=testdata/diff.txt", "--provider=openai", "--output=" + outputFile})

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(&stdoutBuf)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// --output must write to the file only — nothing should appear on stdout.
	if stdoutBuf.Len() > 0 {
		t.Errorf("expected no stdout output when --output is set, got: %q", stdoutBuf.String())
	}

	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("expected output file, got error %v", err)
	}
	if !strings.Contains(string(data), "mocked comment") {
		t.Fatalf("expected mocked comment in file")
	}
}

func TestNewRootCmd_OutputToFileIncludesGeneratedTitle(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	outputFile := filepath.Join(t.TempDir(), "review.md")

	fn := func(_ context.Context, _ *Config, _ ApiProvider, systemPrompt, _ string) (string, error) {
		if strings.HasPrefix(systemPrompt, "Generate a single-line MR/PR title") {
			return "Add generated title", nil
		}
		return "Generated description", nil
	}

	var stdoutBuf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--title", "--file=testdata/diff.txt", "--provider=openai", "--output=" + outputFile})
	cmd.SetOut(&stdoutBuf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if stdoutBuf.Len() > 0 {
		t.Errorf("expected no stdout output when --output is set, got: %q", stdoutBuf.String())
	}

	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("expected output file, got error %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "Add generated title") || !strings.Contains(got, "Generated description") {
		t.Fatalf("expected title and description in output file, got %q", got)
	}
}

func TestNewRootCmd_CustomTemplateFlagUsesTemplateFile(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	tmp := t.TempDir()
	if err := os.Mkdir(tmp+"/templates", 0o755); err != nil {
		t.Fatalf("failed to create templates dir: %v", err)
	}
	if err := os.WriteFile(tmp+"/templates/custom.tmpl", []byte("custom review prompt"), 0o644); err != nil {
		t.Fatalf("failed to write custom template: %v", err)
	}
	diffPath := tmp + "/change.diff"
	if err := os.WriteFile(diffPath, []byte("diff --git a/a.txt b/a.txt\n+++ b/a.txt\n+hello\n"), 0o644); err != nil {
		t.Fatalf("failed to write diff: %v", err)
	}
	t.Chdir(tmp)

	var capturedPrompt string
	fn := func(_ context.Context, _ *Config, _ ApiProvider, systemPrompt, _ string) (string, error) {
		capturedPrompt = systemPrompt
		return "mocked comment", nil
	}

	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--template=custom", "--file=" + diffPath, "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected custom template to be accepted, got %v", err)
	}
	if capturedPrompt != "custom review prompt" {
		t.Fatalf("expected custom prompt, got %q", capturedPrompt)
	}
}

func TestRootCmd_FileDashReadsStdin(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	var capturedDiff string
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, diffContent string) (string, error) {
		capturedDiff = diffContent
		return "pipe review", nil
	}

	var out strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--file=-", "--provider=openai", "--plain"})
	cmd.SetIn(strings.NewReader("diff --git a/a b/a\n+from stdin\n"))
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(capturedDiff, "+from stdin") {
		t.Fatalf("expected stdin diff to reach chat function, got %q", capturedDiff)
	}
	if strings.TrimSpace(out.String()) != "pipe review" {
		t.Fatalf("expected plain review output, got %q", out.String())
	}
}

func TestRootCmd_ImplicitPipeReadsStdin(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	var capturedDiff string
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, diffContent string) (string, error) {
		capturedDiff = diffContent
		return "implicit pipe review", nil
	}

	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--provider=openai", "--plain"})
	cmd.SetIn(strings.NewReader("diff --git a/a b/a\n+implicit stdin\n"))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(capturedDiff, "+implicit stdin") {
		t.Fatalf("expected implicit stdin diff, got %q", capturedDiff)
	}
	if strings.HasPrefix(capturedDiff, "Branch: ") {
		t.Fatalf("stdin diffs should not receive local branch prefix, got %q", capturedDiff)
	}
}

func TestRootCmd_JSONInputMode(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	var capturedDiff string
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, diffContent string) (string, error) {
		capturedDiff = diffContent
		return "json input review", nil
	}

	input := `{"title":"Add API","description":"Adds an endpoint","branch":"feat/API-1","diff":"diff --git a/api.go b/api.go\n+handler\n"}`
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--input=json", "--provider=openai", "--plain"})
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	for _, want := range []string{"Branch: feat/API-1", "PR Title: Add API", "PR Description: Adds an endpoint", "+handler"} {
		if !strings.Contains(capturedDiff, want) {
			t.Fatalf("expected JSON input to include %q, got %q", want, capturedDiff)
		}
	}
}

func TestRootCmd_QuietForcesStrictJSON(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	fn := func(_ context.Context, _ *Config, _ ApiProvider, systemPrompt, _ string) (string, error) {
		if systemPrompt == titlePrompt {
			return "Generated title", nil
		}
		return "quiet review", nil
	}

	var out strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--quiet", "--file=-", "--provider=openai"})
	cmd.SetIn(strings.NewReader("diff --git a/a b/a\n+quiet\n"))
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if strings.Contains(out.String(), "──") {
		t.Fatalf("quiet output must not include text decorations, got %q", out.String())
	}
	var payload struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Provider    string `json:"provider"`
		DiffSource  string `json:"diff_source"`
	}
	if err := json.NewDecoder(strings.NewReader(out.String())).Decode(&payload); err != nil {
		t.Fatalf("expected JSON output, got %v: %s", err, out.String())
	}
	if payload.Title != "Generated title" || payload.Description != "quiet review" || payload.Provider != "openai" || payload.DiffSource != "stdin" {
		t.Fatalf("unexpected quiet payload: %+v", payload)
	}
}

func TestRootCmd_PrintPromptAndRequestDoNotCallProvider(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	called := false
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		called = true
		return "", nil
	}

	var promptOut strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--print-prompt", "--file=-", "--provider=openai"})
	cmd.SetIn(strings.NewReader("diff --git a/a b/a\n+prompt\n"))
	cmd.SetOut(&promptOut)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if called {
		t.Fatal("provider should not be called for --print-prompt")
	}
	if !strings.Contains(promptOut.String(), "MR/PR") && !strings.Contains(strings.ToLower(promptOut.String()), "pull request") {
		t.Fatalf("expected resolved prompt text, got %q", promptOut.String())
	}

	var requestOut strings.Builder
	cmd = newRootCmd(fn)
	cmd.SetArgs([]string{"--print-request", "--file=-", "--provider=openai"})
	cmd.SetIn(strings.NewReader("diff --git a/a b/a\n+request\n"))
	cmd.SetOut(&requestOut)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	var request struct {
		Provider   string `json:"provider"`
		Diff       string `json:"diff"`
		DiffSource string `json:"diff_source"`
	}
	if err := json.NewDecoder(strings.NewReader(requestOut.String())).Decode(&request); err != nil {
		t.Fatalf("expected request JSON, got %v: %s", err, requestOut.String())
	}
	if request.Provider != "openai" || request.DiffSource != "stdin" || !strings.Contains(request.Diff, "+request") {
		t.Fatalf("unexpected request payload: %+v", request)
	}
}

func TestRootCmd_JSONLStream(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		return "fallback review", nil
	}
	deps := defaultCommandDeps()
	deps.streamToWriter = func(_ context.Context, _ *Config, _ ApiProvider, _, _ string, w io.Writer) (string, error) {
		if _, err := io.WriteString(w, "streamed "); err != nil {
			return "", err
		}
		if _, err := io.WriteString(w, "review"); err != nil {
			return "", err
		}
		return "streamed review", nil
	}

	var out strings.Builder
	cmd := newRootCmdWithDeps(fn, deps)
	cmd.SetArgs([]string{"--stream=jsonl", "--file=-", "--provider=openai"})
	cmd.SetIn(strings.NewReader("diff --git a/a b/a\n+stream\n"))
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 JSONL events, got %d: %q", len(lines), out.String())
	}
	var events []map[string]any
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid JSONL line %q: %v", line, err)
		}
		events = append(events, event)
	}
	if events[0]["type"] != "start" || events[1]["type"] != "token" || events[2]["type"] != "token" || events[3]["type"] != "done" {
		t.Fatalf("unexpected event types: %#v", events)
	}
	if events[1]["text"] != "streamed " || events[2]["text"] != "review" {
		t.Fatalf("expected streamed token text, got %#v", events)
	}
}

func TestAgentSubcommandsSupportPipes(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	tests := []struct {
		name string
		args []string
		fn   func(context.Context, *Config, ApiProvider, string, string) (string, error)
		want string
	}{
		{
			name: "review",
			args: []string{"review", "--file=-", "--provider=openai", "--plain"},
			fn: func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
				return "review body", nil
			},
			want: "review body",
		},
		{
			name: "title",
			args: []string{"title", "--file=-", "--provider=openai"},
			fn: func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
				if prompt != titlePrompt {
					t.Fatalf("title subcommand used wrong prompt")
				}
				return "Only title", nil
			},
			want: "Only title",
		},
		{
			name: "commit-message",
			args: []string{"commit-message", "--file=-", "--provider=openai"},
			fn: func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
				return "fix: pipe commit", nil
			},
			want: "fix: pipe commit",
		},
		{
			name: "estimate",
			args: []string{"estimate", "--file=-", "--provider=openai"},
			fn: func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
				t.Fatalf("estimate subcommand should not call provider")
				return "", nil
			},
			want: "Token & Cost Estimation:",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			cmd := newRootCmd(tc.fn)
			cmd.SetArgs(tc.args)
			cmd.SetIn(strings.NewReader("diff --git a/a b/a\n+subcommand\n"))
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true

			if err := cmd.Execute(); err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("expected output to contain %q, got %q", tc.want, out.String())
			}
		})
	}
}

func TestAgentVerdictSubcommandReturnsExitCode(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		return "VERDICT: FAIL\nbad", nil
	}

	var out strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"verdict", "--file=-", "--provider=openai"})
	cmd.SetIn(strings.NewReader("diff --git a/a b/a\n+bad\n"))
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	var exitErr exitCodeError
	if !errors.As(err, &exitErr) || exitErr != 2 {
		t.Fatalf("expected exit code 2, got %T %v", err, err)
	}
	if strings.TrimSpace(out.String()) != "FAIL" {
		t.Fatalf("expected verdict-only output, got %q", out.String())
	}
}

func TestRootCmd_ExitCodeContracts(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--format=xml", "--file=-", "--provider=openai"})
	cmd.SetIn(strings.NewReader("diff --git a/a b/a\n+x\n"))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	var coded codedError
	if !errors.As(err, &coded) || coded.ExitCode() != 4 {
		t.Fatalf("expected invalid usage exit code 4, got %T %v", err, err)
	}

	cmd = newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--file=-", "--provider=openai"})
	cmd.SetIn(strings.NewReader("   \n"))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err = cmd.Execute()
	if !errors.As(err, &coded) || coded.ExitCode() != 3 {
		t.Fatalf("expected no-input exit code 3, got %T %v", err, err)
	}
}

func TestE2E_PipeDiffToQuietJSONReview(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	fn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, diffContent string) (string, error) {
		if prompt == titlePrompt {
			return "Pipe title", nil
		}
		if !strings.Contains(diffContent, "+e2e") {
			t.Fatalf("expected piped diff content, got %q", diffContent)
		}
		return "Pipe description", nil
	}

	var out strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--quiet", "--provider=openai"})
	cmd.SetIn(strings.NewReader("diff --git a/e2e.go b/e2e.go\n+e2e\n"))
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out.String()), &payload); err != nil {
		t.Fatalf("expected JSON payload, got %v: %s", err, out.String())
	}
	if payload["title"] != "Pipe title" || payload["description"] != "Pipe description" || payload["diff_source"] != "stdin" {
		t.Fatalf("unexpected e2e payload: %#v", payload)
	}
}

func TestE2E_JSONInputToCommitMessageSubcommand(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	var capturedDiff string
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, diffContent string) (string, error) {
		capturedDiff = diffContent
		return "feat: accept structured input", nil
	}

	var out strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"commit-message", "--input=json", "--provider=openai"})
	cmd.SetIn(strings.NewReader(`{"branch":"feat/PIPE-9","diff":"diff --git a/a b/a\n+json e2e\n"}`))
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out.String()) != "feat: accept structured input" {
		t.Fatalf("unexpected commit-message output: %q", out.String())
	}
	if !strings.Contains(capturedDiff, "Branch: feat/PIPE-9") || !strings.Contains(capturedDiff, "+json e2e") {
		t.Fatalf("structured input did not reach provider as expected: %q", capturedDiff)
	}
}

func TestChangelog_FileDashReadsStdin(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	var capturedDiff string
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, diffContent string) (string, error) {
		capturedDiff = diffContent
		return "### Changed\n- Piped changelog.", nil
	}

	var out strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"changelog", "--file=-", "--provider=openai"})
	cmd.SetIn(strings.NewReader("diff --git a/a b/a\n+changelog pipe\n"))
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(capturedDiff, "+changelog pipe") {
		t.Fatalf("expected changelog stdin diff, got %q", capturedDiff)
	}
	if !strings.Contains(out.String(), "Piped changelog") {
		t.Fatalf("expected changelog output, got %q", out.String())
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
	if err == nil || !strings.Contains(err.Error(), "OpenAI API key") {
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
	if err == nil || !strings.Contains(err.Error(), "Anthropic API key") {
		t.Fatalf("expected missing API key error, got %v", err)
	}
}

func TestNoConfigFile_ProviderEnvVarWorks(t *testing.T) {
	// Verify that each provider works when only the API key env var is set and
	// there is no config file — the defaults in config.go must be sufficient.
	cases := []struct {
		provider string
		envKey   string
	}{
		{"openai", "OPENAI_API_KEY"},
		{"anthropic", "ANTHROPIC_API_KEY"},
		{"gemini", "GEMINI_API_KEY"},
		{"ollama", ""},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			if tc.envKey != "" {
				t.Setenv(tc.envKey, "dummy-key")
			}
			cmd := newRootCmd(dummyChatFn)
			cmd.SetArgs([]string{"--file=testdata/simple.diff", "--provider=" + tc.provider})
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			if err := cmd.Execute(); err != nil {
				t.Fatalf("expected no error with no config file and provider=%s, got %v", tc.provider, err)
			}
		})
	}
}

func TestDefaultAnthropicEndpointHasTrailingSlash(t *testing.T) {
	// Anthropic SDK WithBaseURL uses url.Parse relative resolution which strips
	// the last path segment if there is no trailing slash, causing doubled paths
	// like /v1/messages/v1/messages. The default must have a trailing slash.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "dummy")

	var capturedCfg *Config
	chatFn := func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
		capturedCfg = cfg
		return "ok", nil
	}
	cmd := newRootCmd(chatFn)
	cmd.SetArgs([]string{"--file=testdata/simple.diff", "--provider=anthropic"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedCfg == nil {
		t.Fatal("chatFn was not called")
	}
	if !strings.HasSuffix(capturedCfg.AnthropicEndpoint, "/") {
		t.Errorf("AnthropicEndpoint default must end with '/'; got %q", capturedCfg.AnthropicEndpoint)
	}
}

func TestNewRootCmd_UnknownTemplateFallsBackToDefault(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--file=testdata/diff.txt", "--provider=openai", "--template=nonexistent"})

	var errBuf strings.Builder
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(&errBuf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected unknown template to fall back to default, got %v", err)
	}
	if !strings.Contains(errBuf.String(), "falling back to default") {
		t.Fatalf("expected fallback warning, got %q", errBuf.String())
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
		{ClaudeCLI, Config{Provider: ClaudeCLI, ClaudeCLIModel: "claude-sonnet-4-6"}, "claude-sonnet-4-6"},
		{GeminiCLI, Config{Provider: GeminiCLI, GeminiCLIModel: "gemini-2.5-flash"}, "gemini-2.5-flash"},
		{CodexCLI, Config{Provider: CodexCLI, CodexCLIModel: "codex-mini"}, "codex-mini"},
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
	var buf strings.Builder
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"completion", "bash"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(buf.String(), "ai-mr-comment") {
		t.Error("expected completion output to contain 'ai-mr-comment'")
	}
}

func TestCompletionScriptsForBashAndZsh(t *testing.T) {
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			var buf strings.Builder
			cmd := newRootCmd(dummyChatFn)
			cmd.SetArgs([]string{"completion", shell})
			cmd.SetOut(&buf)
			cmd.SetErr(io.Discard)
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true

			if err := cmd.Execute(); err != nil {
				t.Fatalf("completion %s failed: %v", shell, err)
			}
			out := buf.String()
			if !strings.Contains(out, "ai-mr-comment") {
				t.Fatalf("expected %s completion output to mention ai-mr-comment", shell)
			}
		})
	}
}

func TestQuickCommitQoLFlagsRegisteredForCompletion(t *testing.T) {
	cmd := newRootCmd(dummyChatFn)
	quickCommit, _, err := cmd.Find([]string{"quick-commit"})
	if err != nil {
		t.Fatalf("find quick-commit: %v", err)
	}
	for _, want := range []string{"edit", "type", "scope", "message-template", "include-untracked", "tracked-only", "signoff"} {
		if quickCommit.Flags().Lookup(want) == nil {
			t.Fatalf("quick-commit flag %q is not registered", want)
		}
	}
}

func TestQuickCommitTypeCompletion(t *testing.T) {
	cmd := newRootCmd(dummyChatFn)
	quickCommit, _, err := cmd.Find([]string{"quick-commit"})
	if err != nil {
		t.Fatalf("find quick-commit: %v", err)
	}
	completion, ok := quickCommit.GetFlagCompletionFunc("type")
	if !ok {
		t.Fatal("missing quick-commit --type completion")
	}
	values, directive := completion(quickCommit, nil, "f")
	if directive == 0 || !containsString(values, "fix") || !containsString(values, "feat") {
		t.Fatalf("expected type completions to include feat/fix, got %v directive=%v", values, directive)
	}
}

func TestQuickCommitMessageTemplateCompletion(t *testing.T) {
	cmd := newRootCmd(dummyChatFn)
	quickCommit, _, err := cmd.Find([]string{"quick-commit"})
	if err != nil {
		t.Fatalf("find quick-commit: %v", err)
	}
	completion, ok := quickCommit.GetFlagCompletionFunc("message-template")
	if !ok {
		t.Fatal("missing quick-commit --message-template completion")
	}
	values, directive := completion(quickCommit, nil, "d")
	if directive == 0 || !containsString(values, "detailed") {
		t.Fatalf("expected message-template completions, got %v directive=%v", values, directive)
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
	_, err := loadConfigWith(v, "")
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
	if err == nil || !strings.Contains(err.Error(), "Gemini API key") {
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
		{ClaudeCLI, "claude-opus-4-6", func(c *Config) string { return c.ClaudeCLIModel }},
		{GeminiCLI, "gemini-2.5-pro", func(c *Config) string { return c.GeminiCLIModel }},
		{CodexCLI, "codex-mini", func(c *Config) string { return c.CodexCLIModel }},
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
	for _, provider := range []string{"openai", "anthropic", "gemini", "ollama", "claude-cli", "gemini-cli", "codex-cli"} {
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
			if provider == "openai" {
				for _, model := range []string{"gpt-5.5", "gpt-5.4-mini"} {
					if !strings.Contains(buf.String(), model) {
						t.Errorf("expected OpenAI model list to include %q, got:\n%s", model, buf.String())
					}
				}
			}
			// codex-cli has no fixed model list; verify the fallback message is shown
			if provider == "codex-cli" && !strings.Contains(buf.String(), "no fixed model list") {
				t.Errorf("expected codex-cli to show 'no fixed model list' message, got:\n%s", buf.String())
			}
		})
	}
}

func TestModelsCmdDefaultsToConfiguredProvider(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, ".ai-mr-comment.toml"), []byte(`provider = "gemini"`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("HOME", tmpDir)

	var buf strings.Builder
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"models"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(buf.String(), "Models for provider gemini") {
		t.Fatalf("expected configured provider in output, got:\n%s", buf.String())
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
	dir := initEmptyRepo(t)
	t.Setenv("OPENAI_API_KEY", "dummy")
	writeRepoFile(t, dir, "branch.txt", "changed\n")
	runGit(t, dir, "add", "branch.txt")

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

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(capturedDiff, "Branch: ") {
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

// TestQuickCommit_DryRun verifies that --dry-run generates and prints the
// commit message without staging, committing, or pushing.
func TestQuickCommit_DryRun(t *testing.T) {
	initRepoWithWorktreeChange(t)
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

	if err := cmd.Execute(); err != nil {
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

func TestQuickCommit_InvalidFormatRejected(t *testing.T) {
	initEmptyRepo(t)
	t.Setenv("OPENAI_API_KEY", "dummy")

	called := false
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		called = true
		return "chore: update config", nil
	}

	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"quick-commit", "--dry-run", "--format=xml", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unsupported format") {
		t.Fatalf("expected unsupported format error, got %v", err)
	}
	if called {
		t.Fatal("chat function should not be called for invalid format")
	}
}

func TestQuickCommit_JSONIncludesProviderAndModel(t *testing.T) {
	initEmptyRepo(t)
	t.Setenv("OPENAI_API_KEY", "dummy")

	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		return "chore: update config", nil
	}

	var buf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"quick-commit", "--chaos", "--dry-run", "--format=json", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result struct {
		CommitMessage string `json:"commit_message"`
		Provider      string `json:"provider"`
		Model         string `json:"model"`
	}
	if err := json.NewDecoder(strings.NewReader(buf.String())).Decode(&result); err != nil {
		t.Fatalf("invalid JSON output: %v\nraw: %s", err, buf.String())
	}
	if result.CommitMessage != "chore: update config" {
		t.Fatalf("expected commit message, got %q", result.CommitMessage)
	}
	if result.Provider != "openai" {
		t.Fatalf("expected provider openai, got %q", result.Provider)
	}
	if result.Model == "" {
		t.Fatal("expected model in JSON output")
	}
}

// TestQuickCommit_DryRun_BranchPrefix verifies that the branch name is
// prepended to the diff content passed to the AI.
func TestQuickCommit_DryRun_BranchPrefix(t *testing.T) {
	initRepoWithWorktreeChange(t)
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

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(capturedDiff, "Branch: ") {
		t.Errorf("expected diffContent to start with 'Branch: ', got:\n%s", capturedDiff)
	}
}

// TestQuickCommit_AIError verifies that an AI error is surfaced correctly.
func TestQuickCommit_AIError(t *testing.T) {
	initRepoWithWorktreeChange(t)
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
	if err == nil {
		t.Fatal("expected an error from AI failure, got nil")
	}
	if !strings.Contains(err.Error(), "AI provider unavailable") {
		t.Errorf("expected AI error message, got: %v", err)
	}
}

// TestQuickCommit_DetachedHead verifies an error is returned in detached HEAD state.
func TestQuickCommit_DetachedHead(t *testing.T) {
	dir := initRepoWithTwoCommits(t)
	runGit(t, dir, "checkout", "--detach", "HEAD")

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

// --- enforceBreakingChange unit tests ---

func TestEnforceBreakingChange(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// Already has ! — unchanged
		{"feat!: add profiles", "feat!: add profiles"},
		{"feat(config)!: add profiles", "feat(config)!: add profiles"},
		// Plain type: rewrite
		{"feat: add profiles", "feat!: add profiles"},
		{"fix: correct typo", "fix!: correct typo"},
		{"chore: bump deps", "chore!: bump deps"},
		// type(scope): rewrite
		{"feat(config): add profiles", "feat(config)!: add profiles"},
		{"fix(api): handle error", "fix(api)!: handle error"},
		// Non-conventional — prefix
		{"add named config profiles", "feat!: add named config profiles"},
	}
	for _, c := range cases {
		got := enforceBreakingChange(c.in)
		if got != c.want {
			t.Errorf("enforceBreakingChange(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestQuickCommit_Breaking_DryRun(t *testing.T) {
	initRepoWithWorktreeChange(t)
	t.Setenv("OPENAI_API_KEY", "dummy")

	// AI returns a plain feat without !, enforceBreakingChange should add it.
	var capturedPrompt, capturedDiff string
	fn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, diff string) (string, error) {
		capturedPrompt = prompt
		capturedDiff = diff
		return "feat(config): add named profiles", nil
	}

	var buf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"quick-commit", "--dry-run", "--breaking", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Prompt must instruct feat!
	if !strings.Contains(capturedPrompt, "feat!") {
		t.Errorf("expected prompt to mention feat!, got:\n%s", capturedPrompt)
	}
	// Diff must contain BREAKING CHANGE footer
	if !strings.Contains(capturedDiff, "BREAKING CHANGE") {
		t.Errorf("expected diff to contain BREAKING CHANGE footer, got:\n%s", capturedDiff)
	}
	// Output message must have ! even though AI omitted it
	if !strings.Contains(buf.String(), "feat(config)!: add named profiles") {
		t.Errorf("expected feat(config)! in output, got:\n%s", buf.String())
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

// TestExitCodeFlag_Fail verifies that the command returns a typed exit-code
// error when the AI returns VERDICT: FAIL.
func TestExitCodeFlag_Fail(t *testing.T) {
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

	err := cmd.Execute()
	var exitErr exitCodeError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected exitCodeError, got %T: %v", err, err)
	}
	if exitErr != 2 {
		t.Errorf("expected exit code 2, got %d", exitErr)
	}
}

// TestExitCodeFlag_MissingVerdictFailsClosed verifies missing verdict lines are
// treated as FAIL and return exit code 2 when --exit-code is set.
func TestExitCodeFlag_MissingVerdictFailsClosed(t *testing.T) {
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

	err := cmd.Execute()
	var exitErr exitCodeError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected exitCodeError, got %T: %v", err, err)
	}
	if exitErr != 2 {
		t.Errorf("expected exit code 2, got %d", exitErr)
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

func TestPostFlag_OutputStillPosts(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	const rawDiff = "diff --git a/foo.go b/foo.go\n+++ b/foo.go\n+fmt.Println(\"hello\")\n"
	var postedBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/owner/repo/pulls/42", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept"), "diff") {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(rawDiff))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"title": "My PR", "body": "Body"})
	})
	mux.HandleFunc("/api/v3/repos/owner/repo/issues/42/comments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var payload struct {
			Body string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("failed to decode posted body: %v", err)
		}
		postedBody = payload.Body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	if err := os.WriteFile(tmpHome+"/.ai-mr-comment.toml", []byte(fmt.Sprintf("github_base_url = %q\n", srv.URL)), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	outFile := t.TempDir() + "/review.txt"
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		return "review body", nil
	}
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--pr=" + srv.URL + "/owner/repo/pull/42", "--post", "--output=" + outFile, "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("expected output file: %v", err)
	}
	if string(data) != "review body" {
		t.Fatalf("expected output file to contain review body, got %q", string(data))
	}
	if postedBody != "review body" {
		t.Fatalf("expected posted body, got %q", postedBody)
	}
}

func TestUpdatePRMetadataFlags_UpdateGitHubTitleAndDescription(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	const rawDiff = "diff --git a/foo.go b/foo.go\n+++ b/foo.go\n+fmt.Println(\"hello\")\n"
	var updated struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/owner/repo/pulls/42", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "diff"):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(rawDiff))
		case r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"title": "Old PR", "body": "Old body"})
		case r.Method == http.MethodPatch:
			if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
				t.Errorf("failed to decode update payload: %v", err)
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 42, "title": updated.Title, "body": updated.Body})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	if err := os.WriteFile(tmpHome+"/.ai-mr-comment.toml", []byte(fmt.Sprintf("github_base_url = %q\n", srv.URL)), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	fn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		if prompt == titlePrompt {
			return "feat: Generated title", nil
		}
		return "Generated PR description", nil
	}
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{
		"--pr=" + srv.URL + "/owner/repo/pull/42",
		"--update-title",
		"--update-description",
		"--provider=openai",
		"--plain",
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if updated.Title != "feat: Generated title" || !strings.Contains(updated.Body, "Generated PR description") {
		t.Fatalf("unexpected updated metadata: %+v", updated)
	}
}

func TestPublishRejectsNoActionsAfterCleaningLists(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{
		"publish",
		"--provider=openai",
		"--no-update-title",
		"--no-update-description",
		"--post-summary=false",
		"--label= , ",
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "publish has no remote actions enabled") {
		t.Fatalf("expected no-actions error, got %v", err)
	}
}

func TestMergeManagedSectionPreservesManualContent(t *testing.T) {
	existing := "Manual intro\n\n" + managedDescriptionStart + "\nold generated\n" + managedDescriptionEnd + "\n\nManual footer"
	got := mergeManagedSection(existing, "new generated")
	if !strings.Contains(got, "Manual intro") || !strings.Contains(got, "Manual footer") {
		t.Fatalf("manual content was not preserved: %q", got)
	}
	if strings.Contains(got, "old generated") || !strings.Contains(got, "new generated") {
		t.Fatalf("managed content was not replaced: %q", got)
	}
}

func TestPublishCommand_GitHubOneShotSync(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	const rawDiff = "diff --git a/docs/readme.md b/docs/readme.md\n+++ b/docs/readme.md\n+security note\n"
	var updatedPR struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	var updatedComment string
	var labels []string
	var reviewers struct {
		Reviewers []string `json:"reviewers"`
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/owner/repo/pulls/42", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "diff"):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(rawDiff))
		case r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"title": "Old PR",
				"body":  "Manual notes\n\n" + managedDescriptionStart + "\nold generated\n" + managedDescriptionEnd,
			})
		case r.Method == http.MethodPatch:
			if err := json.NewDecoder(r.Body).Decode(&updatedPR); err != nil {
				t.Errorf("failed to decode PR update: %v", err)
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 42, "title": updatedPR.Title, "body": updatedPR.Body})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v3/repos/owner/repo/issues/42/comments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 99, "body": managedCommentMarker + "\nold comment"}})
	})
	mux.HandleFunc("/api/v3/repos/owner/repo/issues/comments/99", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			Body string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("failed to decode comment update: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		updatedComment = payload.Body
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 99, "body": payload.Body})
	})
	mux.HandleFunc("/api/v3/repos/owner/repo/issues/42/labels", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&labels); err != nil {
			t.Errorf("failed to decode labels: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	})
	mux.HandleFunc("/api/v3/repos/owner/repo/pulls/42/requested_reviewers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&reviewers); err != nil {
			t.Errorf("failed to decode reviewers: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"number": 42})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	if err := os.WriteFile(tmpHome+"/.ai-mr-comment.toml", []byte(fmt.Sprintf("github_base_url = %q\n", srv.URL)), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	fn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		if prompt == titlePrompt {
			return "Add security docs", nil
		}
		return "Security risk documentation update", nil
	}
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{
		"publish",
		"--pr=" + srv.URL + "/owner/repo/pull/42",
		"--provider=openai",
		"--format=json",
		"--auto-labels",
		"--label=manual",
		"--reviewer=octocat",
		"--draft-if-risky",
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	if updatedPR.Title != "Draft: Add security docs" {
		t.Fatalf("expected draft title, got %q", updatedPR.Title)
	}
	for _, want := range []string{"Manual notes", "Security risk documentation update"} {
		if !strings.Contains(updatedPR.Body, want) {
			t.Fatalf("expected PR body to contain %q, got %q", want, updatedPR.Body)
		}
	}
	if strings.Contains(updatedPR.Body, "old generated") {
		t.Fatalf("expected old managed body to be replaced, got %q", updatedPR.Body)
	}
	if !strings.Contains(updatedComment, managedCommentMarker) || !strings.Contains(updatedComment, "Security risk documentation update") {
		t.Fatalf("expected managed comment update, got %q", updatedComment)
	}
	for _, want := range []string{"manual", "docs", "security"} {
		if !containsString(labels, want) {
			t.Fatalf("expected label %q in %v", want, labels)
		}
	}
	if !containsString(reviewers.Reviewers, "octocat") {
		t.Fatalf("expected reviewer octocat, got %+v", reviewers)
	}
}

func TestUpdatePRMetadataFlags_UpdateGitLabTitleAndDescription(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	const rawDiff = "@@ -1 +1 @@\n-old\n+new\n"
	var updated struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	projectMRPath := "/api/v4/projects/group%2Fsub%2Fproject/merge_requests/5"
	mux := http.NewServeMux()
	mux.HandleFunc(projectMRPath, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"iid":         5,
				"title":       "Old MR",
				"description": "Manual intro\n\n" + managedDescriptionStart + "\nold generated\n" + managedDescriptionEnd,
			})
		case http.MethodPut:
			if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
				t.Errorf("failed to decode GitLab update payload: %v", err)
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"iid": 5, "title": updated.Title, "description": updated.Description})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc(projectMRPath+"/diffs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"old_path": "app.go",
			"new_path": "app.go",
			"diff":     rawDiff,
		}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	if err := os.WriteFile(tmpHome+"/.ai-mr-comment.toml", []byte(fmt.Sprintf("gitlab_base_url = %q\n", srv.URL)), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	fn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, diffContent string) (string, error) {
		if !strings.Contains(diffContent, rawDiff) {
			return "", fmt.Errorf("expected GitLab diff to reach provider, got:\n%s", diffContent)
		}
		if prompt == titlePrompt {
			return "feat: Generated GitLab title", nil
		}
		return "Generated GitLab MR description", nil
	}
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{
		"--pr=" + srv.URL + "/group/sub/project/-/merge_requests/5",
		"--update-title",
		"--update-description",
		"--provider=openai",
		"--plain",
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if updated.Title != "feat: Generated GitLab title" {
		t.Fatalf("unexpected GitLab title update: %+v", updated)
	}
	for _, want := range []string{"Manual intro", "Generated GitLab MR description", managedDescriptionStart, managedDescriptionEnd} {
		if !strings.Contains(updated.Description, want) {
			t.Fatalf("expected GitLab description to contain %q, got %q", want, updated.Description)
		}
	}
	if strings.Contains(updated.Description, "old generated") {
		t.Fatalf("expected old managed section to be replaced, got %q", updated.Description)
	}
}

func TestPublishCommand_GitLabOneShotSync(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	const rawDiff = "@@ -0,0 +1 @@\n+security note\n"
	projectMRPath := "/api/v4/projects/group%2Fsub%2Fproject/merge_requests/5"
	var updatedMR struct {
		Title       string
		Description string
	}
	var updatedNote string
	var labelPayload string
	var reviewerPayload string

	mux := http.NewServeMux()
	mux.HandleFunc(projectMRPath, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"iid":         5,
				"title":       "Old MR",
				"description": "Manual notes\n\n" + managedDescriptionStart + "\nold generated\n" + managedDescriptionEnd,
			})
		case http.MethodPut:
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("failed to decode GitLab publish payload: %v", err)
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if title, ok := payload["title"].(string); ok {
				updatedMR.Title = title
			}
			if description, ok := payload["description"].(string); ok {
				updatedMR.Description = description
			}
			if labels, ok := payload["add_labels"]; ok {
				labelPayload = fmt.Sprint(labels)
			}
			if reviewers, ok := payload["reviewer_ids"]; ok {
				reviewerPayload = fmt.Sprint(reviewers)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"iid":         5,
				"title":       updatedMR.Title,
				"description": updatedMR.Description,
			})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc(projectMRPath+"/diffs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"old_path": "docs/security.md",
			"new_path": "docs/security.md",
			"new_file": true,
			"diff":     rawDiff,
		}})
	})
	mux.HandleFunc(projectMRPath+"/notes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 7, "body": managedCommentMarker + "\nold comment"}})
	})
	mux.HandleFunc(projectMRPath+"/notes/7", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			Body string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("failed to decode GitLab note payload: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		updatedNote = payload.Body
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 7, "body": payload.Body})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	if err := os.WriteFile(tmpHome+"/.ai-mr-comment.toml", []byte(fmt.Sprintf("gitlab_base_url = %q\n", srv.URL)), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	fn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, diffContent string) (string, error) {
		if !strings.Contains(diffContent, rawDiff) {
			return "", fmt.Errorf("expected GitLab diff to reach provider, got:\n%s", diffContent)
		}
		if prompt == titlePrompt {
			return "Add GitLab security docs", nil
		}
		return "Security risk documentation update", nil
	}
	var out strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{
		"publish",
		"--pr=" + srv.URL + "/group/sub/project/-/merge_requests/5",
		"--provider=openai",
		"--format=json",
		"--auto-labels",
		"--label=manual",
		"--reviewer=7",
		"--draft-if-risky",
	})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	if updatedMR.Title != "Draft: Add GitLab security docs" {
		t.Fatalf("expected draft GitLab title, got %q", updatedMR.Title)
	}
	for _, want := range []string{"Manual notes", "Security risk documentation update"} {
		if !strings.Contains(updatedMR.Description, want) {
			t.Fatalf("expected GitLab MR description to contain %q, got %q", want, updatedMR.Description)
		}
	}
	if strings.Contains(updatedMR.Description, "old generated") {
		t.Fatalf("expected old managed GitLab description to be replaced, got %q", updatedMR.Description)
	}
	if !strings.Contains(updatedNote, managedCommentMarker) || !strings.Contains(updatedNote, "Security risk documentation update") {
		t.Fatalf("expected managed GitLab note update, got %q", updatedNote)
	}
	for _, want := range []string{"manual", "docs", "security"} {
		if !strings.Contains(labelPayload, want) {
			t.Fatalf("expected GitLab label payload to contain %q, got %q", want, labelPayload)
		}
	}
	if !strings.Contains(reviewerPayload, "7") {
		t.Fatalf("expected GitLab reviewer payload to contain 7, got %q", reviewerPayload)
	}
	var payload struct {
		URL                string `json:"url"`
		DescriptionUpdated bool   `json:"description_updated"`
		CommentUpserted    bool   `json:"comment_upserted"`
	}
	if err := json.Unmarshal([]byte(out.String()), &payload); err != nil {
		t.Fatalf("expected publish JSON, got %v: %s", err, out.String())
	}
	if payload.URL == "" || !payload.DescriptionUpdated || !payload.CommentUpserted {
		t.Fatalf("unexpected publish payload: %+v", payload)
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
		"alias amc-staged=",
		"alias amc-commit=",
		"alias amc-commit-multi=",
		"alias amc-qc=",
		"alias amc-qc-dry=",
		"alias amc-qc-edit=",
		"alias amc-qc-local=",
		"alias amc-qc-tracked=",
		"alias amc-qc-signoff=",
		"alias amc-qc-fix=",
		"alias amc-qc-docs=",
		"alias amc-qc-detailed=",
		"alias amc-qc-release=",
		"alias amc-qc-ticket=",
		"alias amc-qc-breaking=",
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

func TestGenAliasesShellCompletion(t *testing.T) {
	cmd := newRootCmd(dummyChatFn)
	genAliases, _, err := cmd.Find([]string{"gen-aliases"})
	if err != nil {
		t.Fatalf("find gen-aliases: %v", err)
	}
	completion, ok := genAliases.GetFlagCompletionFunc("shell")
	if !ok {
		t.Fatal("missing shell completion")
	}
	values, directive := completion(genAliases, nil, "z")
	if directive == 0 || !containsString(values, "zsh") {
		t.Fatalf("expected zsh completion, got %v directive=%v", values, directive)
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

// --- --multi-line flag tests ---

func TestNormalizeCommitBody_PreservesStructure(t *testing.T) {
	raw := "feat(auth): add refresh token\n\n## What Changed\n- Added endpoint\n\n## Why\nUsers were logged out."
	got := normalizeCommitBody(raw)
	if got != raw {
		t.Errorf("expected structure preserved, got:\n%s", got)
	}
}

func TestNormalizeCommitBody_StripsFencedCodeBlock(t *testing.T) {
	raw := "```\nfeat(auth): add refresh token\n\n## What Changed\n- Added endpoint\n```"
	got := normalizeCommitBody(raw)
	want := "feat(auth): add refresh token\n\n## What Changed\n- Added endpoint"
	if got != want {
		t.Errorf("expected fence stripped, got:\n%q\nwant:\n%q", got, want)
	}
}

func TestNormalizeCommitBody_NonConventionalSubject(t *testing.T) {
	// No type is injected — the subject is passed through as-is.
	// The prompt (quickCommitPrompt / commitMsgBodyPrompt) is responsible
	// for getting the LLM to produce a conventional subject.
	raw := "add some stuff\n\n## Why\nIt was needed."
	got := normalizeCommitBody(raw)
	if !strings.HasPrefix(got, "add some stuff") {
		t.Errorf("expected subject preserved as-is, got: %q", got)
	}
	if !strings.Contains(got, "## Why") {
		t.Errorf("expected body preserved, got: %q", got)
	}
}

func TestEnforceBreakingChange_MultiLine(t *testing.T) {
	msg := "feat(config): add profiles\n\n## What Changed\n- Added --profile flag"
	got := enforceBreakingChange(msg)
	want := "feat(config)!: add profiles\n\n## What Changed\n- Added --profile flag"
	if got != want {
		t.Errorf("expected:\n%q\ngot:\n%q", want, got)
	}
}

func TestEnforceBreakingChange_MultiLine_BodyUnchanged(t *testing.T) {
	// Body already has ! in subject — should be a no-op
	msg := "feat(config)!: add profiles\n\n## Why\nBreaking."
	got := enforceBreakingChange(msg)
	if got != msg {
		t.Errorf("expected no change when ! already present, got:\n%q", got)
	}
}

func TestCommitMsg_Body_RootCmd(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	multiLine := "feat(auth): add refresh token\n\n## What Changed\n- Added endpoint\n\n## Why\nUsers were logged out."
	fn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		// Verify the body prompt was used (contains "markdown body")
		if !strings.Contains(prompt, "markdown body") {
			return "", fmt.Errorf("expected body prompt, got: %s", prompt)
		}
		return multiLine, nil
	}

	var buf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--commit-msg", "--multi-line", "--file=testdata/simple.diff", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "feat(auth): add refresh token") {
		t.Errorf("expected subject line in output, got:\n%s", out)
	}
	if !strings.Contains(out, "## What Changed") {
		t.Errorf("expected body in output, got:\n%s", out)
	}
}

func TestCommitMsg_Body_RequiresCommitMsg(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--multi-line", "--file=testdata/simple.diff", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --multi-line used without --commit-msg, got nil")
	}
	if !strings.Contains(err.Error(), "--multi-line requires --commit-msg") {
		t.Errorf("expected --multi-line requires --commit-msg error, got: %v", err)
	}
}

func TestMrStyleTemplates_CannotCombineWithCommitMsg(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	for _, tmpl := range []string{
		"technical", "user-focused", "emoji", "sassy", "monday", "jira", "conventional",
		"chaos", "haiku", "roast", "intern", "shakespeare", "manager", "yoda", "excuse",
	} {
		t.Run(tmpl, func(t *testing.T) {
			cmd := newRootCmd(dummyChatFn)
			cmd.SetArgs([]string{"--template=" + tmpl, "--commit-msg", "--file=testdata/simple.diff", "--provider=openai"})
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true

			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected error when --template=%s used with --commit-msg, got nil", tmpl)
			}
			if !strings.Contains(err.Error(), "cannot be combined with --commit-msg") {
				t.Errorf("expected cannot be combined error, got: %v", err)
			}
		})
	}
}

func TestCommitOnlyTemplates_RequireCommitMsg(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	for _, tmpl := range []string{"commit", "commit-emoji", "commit-conventional"} {
		t.Run(tmpl, func(t *testing.T) {
			cmd := newRootCmd(dummyChatFn)
			cmd.SetArgs([]string{"--template=" + tmpl, "--file=testdata/simple.diff", "--provider=openai"})
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true

			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected error when --template=%s used without --commit-msg, got nil", tmpl)
			}
			if !strings.Contains(err.Error(), "--template "+tmpl+" requires --commit-msg") {
				t.Errorf("expected requires --commit-msg error, got: %v", err)
			}
		})
	}
}

func TestQuickCommit_Body_DryRun(t *testing.T) {
	initRepoWithWorktreeChange(t)
	t.Setenv("OPENAI_API_KEY", "dummy")

	multiLine := "feat(config): add profiles\n\n## What Changed\n- Added --profile flag\n\n## Why\nUsers need to switch providers easily."
	fn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		if !strings.Contains(prompt, "markdown body") {
			return "", fmt.Errorf("expected body prompt, got: %s", prompt)
		}
		return multiLine, nil
	}

	var buf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"quick-commit", "--multi-line", "--dry-run", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "feat(config): add profiles") {
		t.Errorf("expected subject line in output, got:\n%s", out)
	}
	if !strings.Contains(out, "## What Changed") {
		t.Errorf("expected body in output, got:\n%s", out)
	}
}

func TestQuickCommit_LongBody_DryRun(t *testing.T) {
	initRepoWithWorktreeChange(t)
	t.Setenv("OPENAI_API_KEY", "dummy")

	fn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		for _, want := range []string{"markdown body", "Long-form body mode", "approximately 32 body lines"} {
			if !strings.Contains(prompt, want) {
				return "", fmt.Errorf("expected prompt to contain %q, got: %s", want, prompt)
			}
		}
		return "feat(config): add profiles\n\n## Summary\n- Added profile support\n\n## Testing\n- Ran go test", nil
	}

	var buf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"quick-commit", "--long", "--body-lines=32", "--dry-run", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "## Summary") {
		t.Fatalf("expected long body output, got:\n%s", buf.String())
	}
}

func TestQuickCommit_MessageTemplate_DetailedImpliesBody(t *testing.T) {
	dir := initEmptyRepo(t)
	t.Setenv("OPENAI_API_KEY", "dummy")

	if err := os.WriteFile(filepath.Join(dir, "template.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "add", ".").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}

	var capturedPrompt string
	fn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		capturedPrompt = prompt
		return "feat(cli): add template flag\n\n## Summary\n- Added template support\n\n## Testing\n- Ran go test", nil
	}

	var buf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"quick-commit", "--message-template=detailed", "--dry-run", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"markdown body", "`detailed` message template", "## Summary", "## Testing"} {
		if !strings.Contains(capturedPrompt, want) && !strings.Contains(buf.String(), want) {
			t.Fatalf("expected prompt or output to contain %q\nprompt:\n%s\noutput:\n%s", want, capturedPrompt, buf.String())
		}
	}
}

func TestApplyCommitTypeScope(t *testing.T) {
	cases := []struct {
		name       string
		msg        string
		forceType  string
		forceScope string
		want       string
	}{
		{
			name:      "force type preserves scope",
			msg:       "fix(api): handle nil response",
			forceType: "chore",
			want:      "chore(api): handle nil response",
		},
		{
			name:       "force scope preserves type",
			msg:        "feat: add quick commit edit mode",
			forceScope: "cli",
			want:       "feat(cli): add quick commit edit mode",
		},
		{
			name:       "non conventional gets conventional prefix",
			msg:        "update generated docs",
			forceType:  "docs",
			forceScope: "examples",
			want:       "docs(examples): update generated docs",
		},
		{
			name:       "body is preserved",
			msg:        "fix: avoid nil panic\n\n## Why\nThe guard was missing.",
			forceScope: "review",
			want:       "fix(review): avoid nil panic\n\n## Why\nThe guard was missing.",
		},
		{
			name:       "breaking marker is preserved",
			msg:        "feat!: remove legacy config",
			forceScope: "config",
			want:       "feat(config)!: remove legacy config",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := applyCommitTypeScope(tc.msg, tc.forceType, tc.forceScope)
			if got != tc.want {
				t.Fatalf("applyCommitTypeScope() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAppendSignedOffBy(t *testing.T) {
	got := appendSignedOffBy("feat(cli): add edit flag", "Test User <test@example.com>")
	want := "feat(cli): add edit flag\n\nSigned-off-by: Test User <test@example.com>"
	if got != want {
		t.Fatalf("appendSignedOffBy() = %q, want %q", got, want)
	}
	if again := appendSignedOffBy(got, "Test User <test@example.com>"); again != got {
		t.Fatalf("appendSignedOffBy should not duplicate trailers, got %q", again)
	}
}

func TestEditCommitMessageWithEditor(t *testing.T) {
	editor := filepath.Join(t.TempDir(), "editor.sh")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nprintf 'fix(cli): edited message\\n\\nEdited body\\n' > \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := editCommitMessageWithEditor("feat(cli): generated message", editor)
	if err != nil {
		t.Fatalf("editCommitMessageWithEditor returned error: %v", err)
	}
	want := "fix(cli): edited message\n\nEdited body"
	if got != want {
		t.Fatalf("edited message = %q, want %q", got, want)
	}
}

func TestQuickCommit_TypeScopeSignoff_NoPush(t *testing.T) {
	dir := initEmptyRepo(t)
	t.Setenv("OPENAI_API_KEY", "dummy")

	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		for _, want := range []string{"Use conventional commit type `fix`", "Use conventional commit scope `cli`"} {
			if !strings.Contains(prompt, want) {
				return "", fmt.Errorf("expected prompt to contain %q, got:\n%s", want, prompt)
			}
		}
		return "feat(api): add generated subject", nil
	}

	var buf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"quick-commit", "--type=fix", "--scope=cli", "--signoff", "--no-push", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("quick-commit failed: %v\noutput:\n%s", err, buf.String())
	}

	out, err := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%B").CombinedOutput()
	if err != nil {
		t.Fatalf("git log failed: %v\n%s", err, out)
	}
	message := string(out)
	for _, want := range []string{"fix(cli): add generated subject", "Signed-off-by: Test <test@example.com>"} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected commit message to contain %q, got:\n%s", want, message)
		}
	}
	if !strings.Contains(buf.String(), "Done. (skipped push)") {
		t.Fatalf("expected no-push output, got:\n%s", buf.String())
	}
}

func TestQuickCommit_TrackedOnlyLeavesUntrackedFiles(t *testing.T) {
	dir := initEmptyRepo(t)
	t.Setenv("OPENAI_API_KEY", "dummy")

	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-C", dir, "add", "."},
		{"-C", dir, "commit", "-m", "initial"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		return "fix(repo): update tracked file", nil
	}
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"quick-commit", "--tracked-only", "--no-push", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("quick-commit failed: %v", err)
	}

	out, err := exec.Command("git", "-C", dir, "status", "--short").CombinedOutput()
	if err != nil {
		t.Fatalf("git status failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "?? untracked.txt") {
		t.Fatalf("expected untracked file to remain untracked, got:\n%s", out)
	}
}

func TestQuickCommit_DryRunIncludesUntrackedOnlyChanges(t *testing.T) {
	dir := initEmptyRepo(t)
	t.Setenv("OPENAI_API_KEY", "dummy")

	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var capturedDiff string
	fn := func(_ context.Context, _ *Config, _ ApiProvider, _, diff string) (string, error) {
		capturedDiff = diff
		return "feat(repo): add untracked file", nil
	}
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"quick-commit", "--dry-run", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("quick-commit dry-run failed: %v", err)
	}
	if !strings.Contains(capturedDiff, "diff --git a/untracked.txt b/untracked.txt") || !strings.Contains(capturedDiff, "+new") {
		t.Fatalf("expected untracked file in dry-run diff, got:\n%s", capturedDiff)
	}
}

func TestQuickCommit_NewFlagValidation(t *testing.T) {
	initEmptyRepo(t)
	t.Setenv("OPENAI_API_KEY", "dummy")

	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "invalid type",
			args: []string{"quick-commit", "--type=feature", "--dry-run", "--provider=openai"},
			want: "--type must be one of",
		},
		{
			name: "invalid scope",
			args: []string{"quick-commit", "--scope=bad scope", "--dry-run", "--provider=openai"},
			want: "--scope contains invalid character",
		},
		{
			name: "no conventional conflict",
			args: []string{"quick-commit", "--type=fix", "--no-conventional", "--dry-run", "--provider=openai"},
			want: "--type and --scope cannot be combined with --no-conventional",
		},
		{
			name: "staging mode conflict",
			args: []string{"quick-commit", "--include-untracked", "--tracked-only", "--dry-run", "--provider=openai"},
			want: "--include-untracked and --tracked-only are mutually exclusive",
		},
		{
			name: "invalid message template",
			args: []string{"quick-commit", "--message-template=verbose", "--dry-run", "--provider=openai"},
			want: "--message-template must be one of",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newRootCmd(dummyChatFn)
			cmd.SetArgs(tc.args)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true

			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestLongCommitBodyPromptSuffixDefault(t *testing.T) {
	got := longCommitBodyPromptSuffix(0)
	if !strings.Contains(got, "approximately 25 body lines") {
		t.Fatalf("expected default body line target, got:\n%s", got)
	}
}

// --- --emoji flag tests ---

func TestAppendCommitEmoji_Types(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"feat: add login", "feat: add login ✨"},
		{"fix(auth): null pointer", "fix(auth): null pointer 🐛"},
		{"docs: update readme", "docs: update readme 📝"},
		{"style: format code", "style: format code 💄"},
		{"refactor: extract helper", "refactor: extract helper ♻️"},
		{"test: add unit tests", "test: add unit tests 🧪"},
		{"chore: update deps", "chore: update deps 🔧"},
		{"perf: cache results", "perf: cache results ⚡"},
		{"ci: fix workflow", "ci: fix workflow 👷"},
		{"build: upgrade go", "build: upgrade go 🏗️"},
		{"feat!: breaking api change", "feat!: breaking api change 💥"},
		{"feat(scope)!: breaking", "feat(scope)!: breaking 💥"},
		{"unknown: something", "unknown: something 🚀"},
	}
	for _, tc := range cases {
		got := appendCommitEmoji(tc.input)
		if got != tc.want {
			t.Errorf("appendCommitEmoji(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestAppendCommitEmoji_PreservesBody(t *testing.T) {
	msg := "feat: add thing\n\n## Why\nBecause."
	got := appendCommitEmoji(msg)
	want := "feat: add thing ✨\n\n## Why\nBecause."
	if got != want {
		t.Errorf("appendCommitEmoji with body:\ngot:  %q\nwant: %q", got, want)
	}
}

// --- Embedded prompt template tests ---

// TestEmbeddedPromptsNonEmpty verifies that all //go:embed prompt vars were
// populated at build time. An empty string means the embed directive failed
// silently or the file was accidentally emptied.
func TestEmbeddedPromptsNonEmpty(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"commitMsgPrompt", commitMsgPrompt},
		{"quickCommitPrompt", quickCommitPrompt},
		{"quickCommitFreePrompt", quickCommitFreePrompt},
		{"commitMsgBodyPrompt", commitMsgBodyPrompt},
		{"changelogPrompt", changelogPrompt},
		{"defaultPromptTemplate", defaultPromptTemplate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if strings.TrimSpace(tc.content) == "" {
				t.Errorf("%s is empty — embedded template file may be missing or blank", tc.name)
			}
		})
	}
}

// TestQuickCommitUsesConventionalPrompt verifies that quick-commit (default)
// sends quickCommitPrompt to the AI, which instructs type(scope) format.
func TestQuickCommitUsesConventionalPrompt(t *testing.T) {
	initRepoWithWorktreeChange(t)
	t.Setenv("OPENAI_API_KEY", "dummy")

	var capturedPrompt string
	fn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		capturedPrompt = prompt
		return "feat(cli): add flag", nil
	}
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"quick-commit", "--dry-run", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// quickCommitPrompt requires type(scope) format and lists valid types.
	if !strings.Contains(capturedPrompt, "type(scope)") {
		t.Errorf("expected quickCommitPrompt (type(scope) format), got:\n%s", capturedPrompt)
	}
	if !strings.Contains(capturedPrompt, "feat") || !strings.Contains(capturedPrompt, "fix") {
		t.Errorf("expected quickCommitPrompt to list commit types, got:\n%s", capturedPrompt)
	}
}

// TestQuickCommitNoConventionalUsesFreePrompt verifies that --no-conventional
// sends quickCommitFreePrompt, which does not require a type(scope) prefix.
func TestQuickCommitNoConventionalUsesFreePrompt(t *testing.T) {
	initRepoWithWorktreeChange(t)
	t.Setenv("OPENAI_API_KEY", "dummy")

	var capturedPrompt string
	fn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		capturedPrompt = prompt
		return "update config defaults", nil
	}
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"quick-commit", "--no-conventional", "--dry-run", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// quickCommitFreePrompt explicitly says no conventional prefix is required.
	if !strings.Contains(capturedPrompt, "No conventional commits prefix required") {
		t.Errorf("expected quickCommitFreePrompt, got:\n%s", capturedPrompt)
	}
	// Must NOT use the conventional prompt.
	if strings.Contains(capturedPrompt, "type(scope)") {
		t.Errorf("--no-conventional should not send conventional prompt, got:\n%s", capturedPrompt)
	}
}

// TestCommitMsgPromptUsedByRootCmd verifies that --commit-msg sends
// commitMsgPrompt (not the stricter quickCommitPrompt).
func TestCommitMsgPromptUsedByRootCmd(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	var capturedPrompt string
	fn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		capturedPrompt = prompt
		return "fix(auth): handle nil token", nil
	}
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--commit-msg", "--file=testdata/simple.diff", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// commitMsgPrompt mentions Conventional Commits but is shorter/simpler than quickCommitPrompt.
	if !strings.Contains(capturedPrompt, "Conventional Commits format") {
		t.Errorf("expected commitMsgPrompt (mentions Conventional Commits format), got:\n%s", capturedPrompt)
	}
	// Must NOT be the quickCommitPrompt (which has the detailed type guide).
	if strings.Contains(capturedPrompt, "type(scope): description\n\nRules:") {
		t.Errorf("--commit-msg should use commitMsgPrompt, not quickCommitPrompt, got:\n%s", capturedPrompt)
	}
}

// TestQuickCommit_Chaos_DryRun verifies that --chaos sends quickCommitChaosPrompt
// and overrides the diff content with "chaos mode".
func TestQuickCommit_Chaos_DryRun(t *testing.T) {
	initEmptyRepo(t)
	t.Setenv("OPENAI_API_KEY", "dummy")

	var capturedPrompt, capturedDiff string
	fn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, diff string) (string, error) {
		capturedPrompt = prompt
		capturedDiff = diff
		return "chore(gremlins): ask nicely that the gremlins stop eating the cache", nil
	}
	var buf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"quick-commit", "--chaos", "--dry-run", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(capturedPrompt, "pipeline trigger commit") {
		t.Errorf("expected quickCommitChaosPrompt, got:\n%s", capturedPrompt)
	}
	if capturedDiff != "chaos mode" {
		t.Errorf("expected diff to be 'chaos mode', got: %q", capturedDiff)
	}
	if !strings.Contains(buf.String(), "chore(gremlins)") {
		t.Errorf("expected commit message in output, got: %q", buf.String())
	}
}

// TestQuickCommit_Chaos_MutualExclusion verifies that --chaos cannot be combined
// with --multi-line or --no-conventional.
func TestQuickCommit_Chaos_MutualExclusion(t *testing.T) {
	initEmptyRepo(t)
	t.Setenv("OPENAI_API_KEY", "dummy")

	for _, args := range [][]string{
		{"quick-commit", "--chaos", "--multi-line", "--dry-run", "--provider=openai"},
		{"quick-commit", "--chaos", "--no-conventional", "--dry-run", "--provider=openai"},
		{"quick-commit", "--chaos", "--haiku", "--dry-run", "--provider=openai"},
		{"quick-commit", "--chaos", "--roast", "--dry-run", "--provider=openai"},
	} {
		cmd := newRootCmd(dummyChatFn)
		cmd.SetArgs(args)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true

		err := cmd.Execute()
		if err == nil {
			t.Errorf("expected error for args %v, got nil", args)
		}
	}
}

// TestQuickCommit_Haiku_DryRun verifies that --haiku sends quickCommitHaikuPrompt.
func TestQuickCommit_Haiku_DryRun(t *testing.T) {
	initRepoWithWorktreeChange(t)
	t.Setenv("OPENAI_API_KEY", "dummy")

	var capturedPrompt string
	fn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		capturedPrompt = prompt
		return "fix(cache): stale data flees now / fresh values fill the void / TTL restored", nil
	}
	var buf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"quick-commit", "--haiku", "--dry-run", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(capturedPrompt, "5-7-5") {
		t.Errorf("expected quickCommitHaikuPrompt, got:\n%s", capturedPrompt)
	}
	if !strings.Contains(buf.String(), "fix(cache)") {
		t.Errorf("expected haiku commit in output, got: %q", buf.String())
	}
}

// TestQuickCommit_Roast_DryRun verifies that --roast sends quickCommitRoastPrompt.
func TestQuickCommit_Roast_DryRun(t *testing.T) {
	initRepoWithWorktreeChange(t)
	t.Setenv("OPENAI_API_KEY", "dummy")

	var capturedPrompt string
	fn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		capturedPrompt = prompt
		return "refactor(naming): rename 'x' to something a human might understand", nil
	}
	var buf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"quick-commit", "--roast", "--dry-run", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(capturedPrompt, "passive-aggressive") {
		t.Errorf("expected quickCommitRoastPrompt, got:\n%s", capturedPrompt)
	}
	if !strings.Contains(buf.String(), "refactor(naming)") {
		t.Errorf("expected roast commit in output, got: %q", buf.String())
	}
}

// TestQuickCommit_Fortune_DryRun verifies that --fortune makes a second AI call
// and appends the fortune to the output.
func TestQuickCommit_Fortune_DryRun(t *testing.T) {
	initRepoWithWorktreeChange(t)
	t.Setenv("OPENAI_API_KEY", "dummy")

	callCount := 0
	fn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		callCount++
		if strings.Contains(prompt, "fortune-cookie") {
			return "The best code is the code you didn't have to write.", nil
		}
		return "feat(api): add rate limiting", nil
	}
	var buf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"quick-commit", "--fortune", "--dry-run", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if callCount != 2 {
		t.Errorf("expected 2 AI calls (commit + fortune), got %d", callCount)
	}
	output := buf.String()
	if !strings.Contains(output, "feat(api)") {
		t.Errorf("expected commit message in output, got: %q", output)
	}
	if !strings.Contains(output, "best code") {
		t.Errorf("expected fortune in output, got: %q", output)
	}
}

// TestRootCmd_Chaos_UsesChaosMRPrompt verifies that --chaos on the root command
// sends mrChaosPrompt as the system prompt.
func TestRootCmd_Chaos_UsesChaosMRPrompt(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	var capturedPrompt string
	fn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		capturedPrompt = prompt
		return "### What Even Is This\n\nChaos reigns.", nil
	}
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--chaos", "--file=testdata/simple.diff", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(capturedPrompt, "chaotic") && !strings.Contains(capturedPrompt, "technically accurate") {
		t.Errorf("expected mrChaosPrompt, got:\n%s", capturedPrompt)
	}
}

// TestRootCmd_Haiku_UsesHaikuMRPrompt verifies that --haiku on the root command
// sends mrHaikuPrompt as the system prompt.
func TestRootCmd_Haiku_UsesHaikuMRPrompt(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	var capturedPrompt string
	fn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		capturedPrompt = prompt
		return "### Summary Haiku\n\ncode changes arrive\nsilently the diff is merged\nall tests still pass green", nil
	}
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--haiku", "--file=testdata/simple.diff", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(capturedPrompt, "5-7-5") {
		t.Errorf("expected mrHaikuPrompt, got:\n%s", capturedPrompt)
	}
}

// TestRootCmd_Roast_UsesRoastMRPrompt verifies that --roast on the root command
// sends mrRoastPrompt as the system prompt.
func TestRootCmd_Roast_UsesRoastMRPrompt(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	var capturedPrompt string
	fn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		capturedPrompt = prompt
		return "### Summary\n\nSomebody did something, apparently.", nil
	}
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--roast", "--file=testdata/simple.diff", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(capturedPrompt, "sardonic") && !strings.Contains(capturedPrompt, "senior engineer") {
		t.Errorf("expected mrRoastPrompt, got:\n%s", capturedPrompt)
	}
}

// TestRootCmd_FunFlags_MutualExclusion verifies that funky style flags are
// mutually exclusive with each other and with --template/--system-prompt/--commit-msg.
func TestRootCmd_FunFlags_MutualExclusion(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	cases := [][]string{
		{"--chaos", "--haiku", "--file=testdata/simple.diff", "--provider=openai"},
		{"--chaos", "--roast", "--file=testdata/simple.diff", "--provider=openai"},
		{"--haiku", "--roast", "--file=testdata/simple.diff", "--provider=openai"},
		{"--chaos", "--template=sassy", "--file=testdata/simple.diff", "--provider=openai"},
		{"--haiku", "--system-prompt=hello", "--file=testdata/simple.diff", "--provider=openai"},
		{"--roast", "--commit-msg", "--file=testdata/simple.diff", "--provider=openai"},
	}

	for _, args := range cases {
		cmd := newRootCmd(dummyChatFn)
		cmd.SetArgs(args)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true

		err := cmd.Execute()
		if err == nil {
			t.Errorf("expected error for args %v, got nil", args)
		}
	}
}

// ── gen-workflow tests ────────────────────────────────────────────────────────

func TestGenWorkflow_Stdout(t *testing.T) {
	cmd := newRootCmd(dummyChatFn)
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"gen-workflow", "--provider=anthropic", "--output=-"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"pull_request",
		"ANTHROPIC_API_KEY",
		"--provider anthropic",
		"GITHUB_TOKEN",
		"ai-mr-comment-linux-amd64",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestGenWorkflow_AllProviders(t *testing.T) {
	cases := []struct {
		provider   string
		wantSecret string
	}{
		{"openai", "OPENAI_API_KEY"},
		{"anthropic", "ANTHROPIC_API_KEY"},
		{"gemini", "GEMINI_API_KEY"},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			cmd := newRootCmd(dummyChatFn)
			var out strings.Builder
			cmd.SetOut(&out)
			cmd.SetArgs([]string{"gen-workflow", "--provider=" + tc.provider, "--output=-"})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(out.String(), tc.wantSecret) {
				t.Errorf("expected %q in output for provider %q", tc.wantSecret, tc.provider)
			}
		})
	}
}

func TestGenWorkflow_InvalidProvider(t *testing.T) {
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"gen-workflow", "--provider=ollama", "--output=-"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for unsupported provider, got nil")
	}
}

func TestGenWorkflow_FileOutput(t *testing.T) {
	dir := t.TempDir()
	outputPath := dir + "/workflow.yml"
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"gen-workflow", "--provider=openai", "--output=" + outputPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if !strings.Contains(string(data), "OPENAI_API_KEY") {
		t.Error("generated file missing OPENAI_API_KEY")
	}
}

func TestQuickCommit_PostIncompatibleFlags(t *testing.T) {
	incompatible := [][]string{
		{"quick-commit", "--post", "--dry-run"},
		{"quick-commit", "--post", "--no-push"},
	}
	for _, args := range incompatible {
		cmd := newRootCmd(dummyChatFn)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Errorf("expected error for args %v, got nil", args)
		}
	}
}

func TestValidateProviderConfig_CLIProvidersAccepted(t *testing.T) {
	// CLI providers require no API key; validateProviderConfig must not reject them.
	for _, provider := range []ApiProvider{ClaudeCLI, GeminiCLI, CodexCLI} {
		t.Run(string(provider), func(t *testing.T) {
			cfg := &Config{Provider: provider}
			if err := validateProviderConfig(cfg); err != nil {
				t.Errorf("expected no error for provider %s, got %v", provider, err)
			}
		})
	}
}

func TestMaskSecret(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "(not set)"},
		{"ab", "****"},
		{"abcd", "****"},
		{"abcde", "abcd****"},
		{"sk-ant-abc123", "sk-a****"},
	}
	for _, tc := range cases {
		if got := maskSecret(tc.in); got != tc.want {
			t.Errorf("maskSecret(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCheckCmd_Success(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy-key")
	var capturedPrompt string
	chatFn := func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
		capturedPrompt = systemPrompt
		return "OK", nil
	}
	cmd := newRootCmd(chatFn)
	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"check", "--provider=anthropic"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "OK") {
		t.Errorf("expected OK in output, got: %s", out)
	}
	if !strings.Contains(capturedPrompt, "OK") {
		t.Errorf("expected ping prompt to contain 'OK', got: %q", capturedPrompt)
	}
}

func TestCheckCmd_APIError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy-key")
	chatFn := func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
		return "", errors.New("connection refused")
	}
	cmd := newRootCmd(chatFn)
	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"check", "--provider=anthropic"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "check failed") {
		t.Errorf("expected 'check failed' in error, got: %v", err)
	}
	if !strings.Contains(buf.String(), "FAIL") {
		t.Errorf("expected FAIL in output, got: %s", buf.String())
	}
}

func TestCheckCmd_MissingAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	cmd := newRootCmd(dummyChatFn)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"check", "--provider=anthropic"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing API key, got nil")
	}
	if !strings.Contains(err.Error(), "config error") {
		t.Errorf("expected 'config error', got: %v", err)
	}
}

func TestCheckCmd_PrintsProviderInfo(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-testkey")
	chatFn := func(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
		return "OK", nil
	}
	cmd := newRootCmd(chatFn)
	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"check", "--provider=openai"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "openai") {
		t.Errorf("expected provider name in output, got: %s", out)
	}
	if !strings.Contains(out, "sk-t****") {
		t.Errorf("expected masked API key in output, got: %s", out)
	}
}

func TestCheckAll_AllPass(t *testing.T) {
	// Set all API keys so no provider is skipped.
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("ANTHROPIC_API_KEY", "ant-test")
	t.Setenv("GEMINI_API_KEY", "gem-test")

	chatFn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		return "OK", nil
	}
	cmd := newRootCmd(chatFn)
	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"check", "--all"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	for _, p := range allProviders {
		if !strings.Contains(out, string(p)) {
			t.Errorf("expected provider %q in output", p)
		}
	}
	if strings.Contains(out, "FAIL") {
		t.Errorf("expected no FAIL in output, got:\n%s", out)
	}
}

func TestCheckAll_SomeFail(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("ANTHROPIC_API_KEY", "ant-test")
	t.Setenv("GEMINI_API_KEY", "gem-test")

	chatFn := func(_ context.Context, _ *Config, p ApiProvider, _, _ string) (string, error) {
		if p == Anthropic {
			return "", errors.New("connection refused")
		}
		return "OK", nil
	}
	cmd := newRootCmd(chatFn)
	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"check", "--all"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when a provider fails, got nil")
	}
	if !strings.Contains(err.Error(), "one or more providers failed") {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "FAIL") {
		t.Errorf("expected FAIL in output, got:\n%s", buf.String())
	}
}

func TestCheckAll_SkipsUnconfigured(t *testing.T) {
	// Clear all keys — every API provider should be skipped, not failed.
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	called := false
	chatFn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		called = true
		return "OK", nil
	}
	cmd := newRootCmd(chatFn)
	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"check", "--all"})
	// CLI providers (claude-cli, gemini-cli, codex-cli) may or may not be present
	// on the test machine, so we only assert API providers are skipped, not failed.
	_ = cmd.Execute()
	out := buf.String()
	for _, p := range []ApiProvider{OpenAI, Anthropic, Gemini} {
		if !strings.Contains(out, "SKIP") {
			t.Errorf("expected SKIP for provider %q when key is unset, got:\n%s", p, out)
		}
	}
	_ = called // CLI providers may still be called if binaries exist
}

func TestCheckAll_TableColumns(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	chatFn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		return "OK", nil
	}
	cmd := newRootCmd(chatFn)
	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"check", "--all"})
	_ = cmd.Execute()
	out := buf.String()
	// Header row must be present.
	if !strings.Contains(out, "PROVIDER") || !strings.Contains(out, "MODEL") || !strings.Contains(out, "STATUS") {
		t.Errorf("expected table header in output, got:\n%s", out)
	}
}

func TestPingProvider_SkipWhenKeyMissing(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	cfg, _ := loadConfigForProfile("")
	chatFn := func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
		t.Fatal("chatFn must not be called when provider is skipped")
		return "", nil
	}
	r := pingProvider(context.Background(), cfg, Anthropic, chatFn)
	if !r.skipped {
		t.Errorf("expected skipped=true when API key is missing")
	}
}

func TestValidateProviderConfig_UnknownProviderRejected(t *testing.T) {
	cfg := &Config{Provider: "bogus"}
	err := validateProviderConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "unsupported provider") {
		t.Errorf("expected unsupported provider error, got %v", err)
	}
}

func TestNewRootCmd_CLIProvider_NoAPIKeyRequired(t *testing.T) {
	// Using a CLI provider with no API key set must not error on key validation.
	// The chatFn is a dummy so no real binary is invoked.
	for _, provider := range []string{"claude-cli", "gemini-cli", "codex-cli"} {
		t.Run(provider, func(t *testing.T) {
			t.Setenv("ANTHROPIC_API_KEY", "")
			t.Setenv("OPENAI_API_KEY", "")
			t.Setenv("GEMINI_API_KEY", "")

			cmd := newRootCmd(dummyChatFn)
			cmd.SetArgs([]string{"--file=testdata/diff.txt", "--provider=" + provider})
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			err := cmd.Execute()
			if err != nil && (strings.Contains(err.Error(), "API key") || strings.Contains(err.Error(), "unsupported provider")) {
				t.Errorf("provider %s should not require API key, got: %v", provider, err)
			}
		})
	}
}
