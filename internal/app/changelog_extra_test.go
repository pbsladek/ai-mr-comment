package app

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestWriteChangelogDryRunText(t *testing.T) {
	cmd := &cobra.Command{}
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	err := writeChangelogDryRun(cmd, &Config{Provider: OpenAI, OpenAIModel: "gpt-5.5"}, changelogArgs{
		format:     "text",
		preset:     "release",
		outputPath: "CHANGELOG.md",
	}, diffSummary{
		FileCount: 2,
		Additions: 10,
		Deletions: 3,
	}, "prompt")
	if err != nil {
		t.Fatalf("writeChangelogDryRun failed: %v", err)
	}
	got := out.String()
	for _, want := range []string{"Dry run:", "- Provider: openai", "- Model: gpt-5.5", "- Preset: release", "- Would write output: CHANGELOG.md"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}
}

func TestApplyChangelogPresetBranches(t *testing.T) {
	tests := []struct {
		name       string
		preset     string
		flag       string
		wantFormat string
		wantModel  string
		wantSuffix string
		wantErr    string
	}{
		{name: "empty", preset: "", wantFormat: "text"},
		{name: "ci sets json", preset: "ci", wantFormat: "json"},
		{name: "local fast sets ollama", preset: "local-fast", wantFormat: "text", wantModel: "llama3.2"},
		{name: "security adds suffix", preset: "security", wantFormat: "text", wantSuffix: "security-relevant changes"},
		{name: "release notes no op", preset: "release-notes", wantFormat: "text"},
		{name: "unknown", preset: "bad", wantFormat: "text", wantErr: "unknown preset"},
		{name: "ci respects changed format", preset: "ci", flag: "format", wantFormat: "yaml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "test"}
			cmd.Flags().String("format", "text", "")
			if tc.flag == "format" {
				if err := cmd.Flags().Set("format", "yaml"); err != nil {
					t.Fatal(err)
				}
			}
			format := "text"
			if tc.flag == "format" {
				format = "yaml"
			}
			cfg := &Config{}
			suffix, err := applyChangelogPreset(cmd, tc.preset, cfg, &format)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected %q error, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("applyChangelogPreset failed: %v", err)
			}
			if format != tc.wantFormat {
				t.Fatalf("format = %q, want %q", format, tc.wantFormat)
			}
			if tc.wantModel != "" && (cfg.Provider != Ollama || cfg.OllamaModel != tc.wantModel) {
				t.Fatalf("local-fast config = provider %q model %q", cfg.Provider, cfg.OllamaModel)
			}
			if tc.wantSuffix != "" && !strings.Contains(suffix, tc.wantSuffix) {
				t.Fatalf("suffix = %q, want to contain %q", suffix, tc.wantSuffix)
			}
		})
	}
}

func TestResolveDiffWithDepsFileBranches(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(""))
	dir := t.TempDir()
	diffPath := filepath.Join(dir, "change.diff")
	if err := os.WriteFile(diffPath, []byte("diff --git a/a.go b/a.go\n+one\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := resolveDiffWithDeps(cmd, "", diffPath, defaultCommandDeps())
	if err != nil {
		t.Fatalf("resolveDiffWithDeps file failed: %v", err)
	}
	if !strings.Contains(got, "diff --git") {
		t.Fatalf("diff content = %q", got)
	}

	if _, err := resolveDiffWithDeps(cmd, "HEAD~1..HEAD", filepath.Join(dir, "empty.diff"), defaultCommandDeps()); err == nil {
		t.Fatal("expected file read error")
	}

	emptyPath := filepath.Join(dir, "empty.diff")
	if err := os.WriteFile(emptyPath, []byte(" \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveDiffWithDeps(cmd, "HEAD~1..HEAD", emptyPath, defaultCommandDeps()); err == nil || !strings.Contains(err.Error(), `commit range "HEAD~1..HEAD"`) {
		t.Fatalf("expected commit range empty-diff error, got %v", err)
	}
	if _, err := resolveDiffWithDeps(cmd, "", emptyPath, defaultCommandDeps()); err == nil || !strings.Contains(err.Error(), "Specify a commit range") {
		t.Fatalf("expected generic empty-diff error, got %v", err)
	}
}

func TestResolveDiffWithDepsGitErrors(t *testing.T) {
	cmd := &cobra.Command{}
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := devNull.Close(); err != nil {
			t.Errorf("closing dev null: %v", err)
		}
	}()
	cmd.SetIn(devNull)

	deps := defaultCommandDeps()
	deps.isGitRepo = func() bool { return false }
	if _, err := resolveDiffWithDeps(cmd, "", "", deps); err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("expected not-git error, got %v", err)
	}

	deps.isGitRepo = func() bool { return true }
	deps.getGitDiff = func(string, bool, []string) (string, error) { return "", errors.New("git failed") }
	if _, err := resolveDiffWithDeps(cmd, "main..HEAD", "", deps); err == nil || !strings.Contains(err.Error(), "git failed") {
		t.Fatalf("expected git diff error, got %v", err)
	}
}
