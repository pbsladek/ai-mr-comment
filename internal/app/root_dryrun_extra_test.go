package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestRootDryRunTextReportsAllPlannedActions(t *testing.T) {
	deps := defaultCommandDeps()
	deps.loadConfig = func(string) (*Config, error) {
		return &Config{Provider: OpenAI, OpenAIAPIKey: "dummy", OpenAIModel: "gpt-5.5", Template: "default"}, nil
	}
	deps.clipboard = func(string) error {
		return errors.New("must not copy during dry-run")
	}
	deps.writeFile = func(string, []byte, os.FileMode) error {
		return errors.New("must not write during dry-run")
	}
	deps.getRemoteDiff = func(_ context.Context, _ *Config, targetURL string) (string, error) {
		if targetURL != "https://github.com/owner/repo/pull/1" {
			return "", errors.New("unexpected target")
		}
		return "diff --git a/file.txt b/file.txt\n+changed\n", nil
	}

	var out strings.Builder
	cmd := newRootCmdWithDeps(func(context.Context, *Config, ApiProvider, string, string) (string, error) {
		return "", errors.New("must not call provider during dry-run")
	}, deps)
	cmd.SetArgs([]string{
		"--dry-run",
		"--provider=openai",
		"--post",
		"--update-title",
		"--update-description",
		"--pr=https://github.com/owner/repo/pull/1",
		"--output=review.md",
		"--clipboard=all",
	})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"Dry run: no provider call",
		"- Provider: openai",
		"- Would write output: review.md",
		"- Would copy clipboard: all",
		"- Would post comment: https://github.com/owner/repo/pull/1",
		"- Would update PR/MR title+description: https://github.com/owner/repo/pull/1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in dry-run output:\n%s", want, got)
		}
	}
}

func TestRootOutputSideEffectsWithRemoteTarget(t *testing.T) {
	deps := defaultCommandDeps()
	deps.loadConfig = func(string) (*Config, error) {
		return &Config{Provider: OpenAI, OpenAIAPIKey: "dummy", OpenAIModel: "gpt-5.5", Template: "default"}, nil
	}
	deps.getRemoteDiff = func(_ context.Context, _ *Config, targetURL string) (string, error) {
		if targetURL != "https://github.com/owner/repo/pull/1" {
			t.Fatalf("unexpected target URL %q", targetURL)
		}
		return "diff --git a/file.txt b/file.txt\n+changed\n", nil
	}
	var clipboardContent string
	deps.clipboard = func(content string) error {
		clipboardContent = content
		return errors.New("clipboard unavailable")
	}
	var filePath, fileContent string
	deps.writeFile = func(path string, content []byte, _ os.FileMode) error {
		filePath = path
		fileContent = string(content)
		return nil
	}
	deps.getRemoteMetadata = func(context.Context, *Config, string) (prMetadata, error) {
		return prMetadata{Description: "Manual section"}, nil
	}
	var updatedTitle, updatedDescription string
	deps.updateRemoteMetadata = func(_ context.Context, _ *Config, _ string, title, description *string) error {
		if title != nil {
			updatedTitle = *title
		}
		if description != nil {
			updatedDescription = *description
		}
		return nil
	}
	var posted string
	deps.postRemoteComment = func(_ context.Context, _ *Config, _ string, body string) error {
		posted = body
		return nil
	}

	var stdout, stderr strings.Builder
	cmd := newRootCmdWithDeps(func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		if prompt == titlePrompt {
			return "Generated title", nil
		}
		return "Generated comment", nil
	}, deps)
	cmd.SetArgs([]string{
		"--provider=openai",
		"--pr=https://github.com/owner/repo/pull/1",
		"--title",
		"--output=review.md",
		"--clipboard=all",
		"--update-title",
		"--update-description",
		"--post",
	})
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("root command failed: %v", err)
	}
	if filePath != "review.md" || !strings.Contains(fileContent, "Generated title\n\nGenerated comment") {
		t.Fatalf("file write = %q %q", filePath, fileContent)
	}
	if !strings.Contains(clipboardContent, "Generated title\n\nGenerated comment") {
		t.Fatalf("clipboard content = %q", clipboardContent)
	}
	if updatedTitle != "Generated title" || !strings.Contains(updatedDescription, managedDescriptionStart) {
		t.Fatalf("updated metadata title=%q description=%q", updatedTitle, updatedDescription)
	}
	if posted != "**Generated title**\n\nGenerated comment" {
		t.Fatalf("posted = %q", posted)
	}
	if !strings.Contains(stderr.String(), "could not copy to clipboard") || !strings.Contains(stderr.String(), "Posted comment") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRootJSONLTitleOnlyAndCommitMessage(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "title", args: []string{"--title-only", "--stream=jsonl"}, want: `"text":"Generated title"`},
		{name: "commit", args: []string{"--commit-msg", "--stream=jsonl"}, want: `"text":"fix: generated"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := defaultCommandDeps()
			deps.loadConfig = func(string) (*Config, error) {
				return &Config{Provider: OpenAI, OpenAIAPIKey: "dummy", OpenAIModel: "gpt-5.5", Template: "default"}, nil
			}
			args := append([]string{"--provider=openai", "--file=testdata/simple.diff"}, tc.args...)
			var out bytes.Buffer
			cmd := newRootCmdWithDeps(func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
				if prompt == titlePrompt {
					return "Generated title", nil
				}
				return "fix: generated", nil
			}, deps)
			cmd.SetArgs(args)
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("root command failed: %v", err)
			}
			got := out.String()
			if !strings.Contains(got, `"type":"start"`) || !strings.Contains(got, tc.want) || !strings.Contains(got, `"type":"done"`) {
				t.Fatalf("unexpected jsonl output:\n%s", got)
			}
		})
	}
}
