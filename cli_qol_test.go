package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootDryRunSkipsProviderAndSideEffects(t *testing.T) {
	called := false
	fn := func(context.Context, *Config, ApiProvider, string, string) (string, error) {
		called = true
		return "should not be called", nil
	}

	outFile := filepath.Join(t.TempDir(), "review.md")
	var buf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--dry-run", "--format=json", "--file=testdata/simple.diff", "--provider=openai", "--output=" + outFile})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if called {
		t.Fatal("dry-run must not call the provider")
	}
	if _, err := os.Stat(outFile); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote output file or stat failed: %v", err)
	}

	var payload struct {
		DryRun           bool   `json:"dry_run"`
		Provider         string `json:"provider"`
		WouldWriteOutput bool   `json:"would_write_output"`
		Summary          struct {
			FileCount int `json:"file_count"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(buf.String()), &payload); err != nil {
		t.Fatalf("invalid dry-run json: %v\n%s", err, buf.String())
	}
	if !payload.DryRun || payload.Provider != "openai" || !payload.WouldWriteOutput || payload.Summary.FileCount == 0 {
		t.Fatalf("unexpected dry-run payload: %+v", payload)
	}
}

func TestRootSummaryOnlyAndChangedFilesSkipProvider(t *testing.T) {
	called := false
	fn := func(context.Context, *Config, ApiProvider, string, string) (string, error) {
		called = true
		return "should not be called", nil
	}

	var summaryOut strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"--summary-only", "--file=testdata/simple.diff", "--provider=openai"})
	cmd.SetOut(&summaryOut)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("summary-only failed: %v", err)
	}
	if called {
		t.Fatal("summary-only must not call the provider")
	}
	if !strings.Contains(summaryOut.String(), "Diff summary") || !strings.Contains(summaryOut.String(), "Changed files:") {
		t.Fatalf("expected text summary, got:\n%s", summaryOut.String())
	}

	var filesOut strings.Builder
	cmd = newRootCmd(fn)
	cmd.SetArgs([]string{"--changed-files", "--file=testdata/simple.diff", "--provider=openai"})
	cmd.SetOut(&filesOut)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("changed-files failed: %v", err)
	}
	if strings.TrimSpace(filesOut.String()) == "" {
		t.Fatal("expected changed file output")
	}
}

func TestSummaryOnlyUsesRawDiffBeforeTruncation(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("diff --git a/first.txt b/first.txt\n--- a/first.txt\n+++ b/first.txt\n@@ -1 +1 @@\n-old\n+new\n")
	for i := 0; i < 4100; i++ {
		fmt.Fprintf(&sb, "+padding %d\n", i)
	}
	sb.WriteString("diff --git a/late.txt b/late.txt\n--- a/late.txt\n+++ b/late.txt\n@@ -1 +1 @@\n-old\n+new\n")

	diffPath := filepath.Join(t.TempDir(), "large.diff")
	if err := os.WriteFile(diffPath, []byte(sb.String()), 0600); err != nil {
		t.Fatalf("write large diff: %v", err)
	}

	var buf strings.Builder
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--summary-only", "--format=json", "--file=" + diffPath, "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("summary-only failed: %v", err)
	}
	var summary diffSummary
	if err := json.Unmarshal([]byte(buf.String()), &summary); err != nil {
		t.Fatalf("invalid summary json: %v\n%s", err, buf.String())
	}
	if !summary.Truncated {
		t.Fatal("expected truncated summary")
	}
	if summary.FileCount != 2 {
		t.Fatalf("expected summary to include files after truncation point, got %+v", summary.Files)
	}
	if summary.Files[1].Path != "late.txt" {
		t.Fatalf("expected late.txt in summary, got %+v", summary.Files)
	}
}

func TestSummarizeDiffHandlesQuotedPathsWithSpaces(t *testing.T) {
	diff := "diff --git \"a/path with spaces.go\" \"b/path with spaces.go\"\n" +
		"--- \"a/path with spaces.go\"\n" +
		"+++ \"b/path with spaces.go\"\n" +
		"@@ -1 +1 @@\n" +
		"-old\n" +
		"+new\n"

	summary := summarizeDiff(diff, "test", "model", false)
	if summary.FileCount != 1 {
		t.Fatalf("expected one file, got %+v", summary.Files)
	}
	if summary.Files[0].Path != "path with spaces.go" {
		t.Fatalf("expected unquoted path with spaces, got %q", summary.Files[0].Path)
	}
	if summary.Files[0].Additions != 1 || summary.Files[0].Deletions != 1 {
		t.Fatalf("unexpected line counts: %+v", summary.Files[0])
	}
}

func TestSummarizeDiffHandlesUnquotedPathsWithSpaces(t *testing.T) {
	diff := "diff --git a/old name.go b/new name.go\n" +
		"--- a/old name.go\n" +
		"+++ b/new name.go\n" +
		"@@ -1 +1 @@\n" +
		"-old\n" +
		"+new\n"

	summary := summarizeDiff(diff, "test", "model", false)
	if summary.FileCount != 1 {
		t.Fatalf("expected one file, got %+v", summary.Files)
	}
	if summary.Files[0].Path != "new name.go" {
		t.Fatalf("expected new path with spaces, got %q", summary.Files[0].Path)
	}
}

func TestSummarizeDiffCountsPlusPlusAndMinusMinusContent(t *testing.T) {
	diff := "diff --git a/flags.txt b/flags.txt\n" +
		"--- a/flags.txt\n" +
		"+++ b/flags.txt\n" +
		"@@ -1 +1 @@\n" +
		"--flag\n" +
		"+++i\n"

	summary := summarizeDiff(diff, "test", "model", false)
	if summary.Additions != 1 || summary.Deletions != 1 {
		t.Fatalf("expected content lines to count, got +%d/-%d", summary.Additions, summary.Deletions)
	}
	if summary.Files[0].Additions != 1 || summary.Files[0].Deletions != 1 {
		t.Fatalf("unexpected file counts: %+v", summary.Files[0])
	}
}

func TestSummarizeDiffHandlesGitBinaryPatch(t *testing.T) {
	diff := "diff --git a/image.png b/image.png\n" +
		"index 111..222 100644\n" +
		"GIT binary patch\n" +
		"literal 3\n" +
		"abc\n"

	summary := summarizeDiff(diff, "test", "model", false)
	if summary.FileCount != 1 || !summary.Files[0].Binary {
		t.Fatalf("expected binary file summary, got %+v", summary.Files)
	}
	if summary.Additions != 0 || summary.Deletions != 0 {
		t.Fatalf("expected binary patch payload to be ignored, got +%d/-%d", summary.Additions, summary.Deletions)
	}
}

func TestSummarizeDiffHandlesHeaderOnlyDeletion(t *testing.T) {
	diff := "--- a/deleted.txt\n" +
		"+++ /dev/null\n" +
		"@@ -1 +0,0 @@\n" +
		"-old\n"

	summary := summarizeDiff(diff, "test", "model", false)
	if summary.FileCount != 1 || summary.Files[0].Path != "deleted.txt" {
		t.Fatalf("expected deleted file path, got %+v", summary.Files)
	}
	if summary.Deletions != 1 {
		t.Fatalf("expected deletion count, got %d", summary.Deletions)
	}
}

func TestCleanDiffPathStripsOnlyOneGitPrefix(t *testing.T) {
	if got := cleanDiffPath("b/a/b/file.go"); got != "a/b/file.go" {
		t.Fatalf("expected one prefix stripped, got %q", got)
	}
}

func TestDoctorConfigDumpJSONMasksSecrets(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test-secret")

	var buf strings.Builder
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"config-dump", "--provider=openai", "--format=json"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("config-dump failed: %v", err)
	}
	if strings.Contains(buf.String(), "sk-test-secret") {
		t.Fatalf("config-dump leaked secret: %s", buf.String())
	}
	var payload struct {
		Provider string            `json:"provider"`
		Secrets  map[string]string `json:"secrets"`
	}
	if err := json.Unmarshal([]byte(buf.String()), &payload); err != nil {
		t.Fatalf("invalid config-dump json: %v\n%s", err, buf.String())
	}
	if payload.Provider != "openai" || payload.Secrets["OPENAI_API_KEY"] != "set" {
		t.Fatalf("unexpected config-dump payload: %+v", payload)
	}
}

func TestSanitizeRemoteURLStripsCredentials(t *testing.T) {
	got := sanitizeRemoteURL("https://token:secret@github.com/owner/repo.git")
	if strings.Contains(got, "token") || strings.Contains(got, "secret") {
		t.Fatalf("remote still contains credentials: %s", got)
	}
	if got != "https://github.com/owner/repo.git" {
		t.Fatalf("unexpected sanitized remote: %s", got)
	}

	sshRemote := "git@github.com:owner/repo.git"
	if got := sanitizeRemoteURL(sshRemote); got != sshRemote {
		t.Fatalf("expected ssh remote unchanged, got %s", got)
	}
}

func TestPresetLocalFastAppliesOllamaWithoutAPIKey(t *testing.T) {
	var buf strings.Builder
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--preset=local-fast", "--print-request", "--file=testdata/simple.diff"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("print-request with local-fast preset failed: %v", err)
	}
	var payload struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Preset   string `json:"preset"`
	}
	if err := json.Unmarshal([]byte(buf.String()), &payload); err != nil {
		t.Fatalf("invalid request json: %v\n%s", err, buf.String())
	}
	if payload.Provider != "ollama" || payload.Model != "llama3.2" || payload.Preset != "local-fast" {
		t.Fatalf("preset not applied: %+v", payload)
	}
}

func TestChangelogDryRunSkipsProviderAndFileWrite(t *testing.T) {
	called := false
	fn := func(context.Context, *Config, ApiProvider, string, string) (string, error) {
		called = true
		return "should not be called", nil
	}

	outFile := filepath.Join(t.TempDir(), "changelog.md")
	var buf strings.Builder
	cmd := newRootCmd(fn)
	cmd.SetArgs([]string{"changelog", "--dry-run", "--format=json", "--file=testdata/simple.diff", "--provider=openai", "--output=" + outFile})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("changelog dry-run failed: %v", err)
	}
	if called {
		t.Fatal("changelog dry-run must not call the provider")
	}
	if _, err := os.Stat(outFile); !os.IsNotExist(err) {
		t.Fatalf("changelog dry-run wrote output file or stat failed: %v", err)
	}
	var payload struct {
		DryRun bool `json:"dry_run"`
	}
	if err := json.Unmarshal([]byte(buf.String()), &payload); err != nil {
		t.Fatalf("invalid changelog dry-run json: %v\n%s", err, buf.String())
	}
	if !payload.DryRun {
		t.Fatalf("expected dry_run=true, got %+v", payload)
	}
}

func TestRootDryRunPostWithoutPRReportsMissingTarget(t *testing.T) {
	var buf strings.Builder
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--dry-run", "--post", "--format=json", "--file=testdata/simple.diff", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("dry-run --post without --pr should not fail: %v", err)
	}
	var payload struct {
		WouldCallProvider bool `json:"would_call_provider"`
		WouldPostComment  bool `json:"would_post_comment"`
		MissingPostTarget bool `json:"missing_post_target"`
	}
	if err := json.Unmarshal([]byte(buf.String()), &payload); err != nil {
		t.Fatalf("invalid dry-run json: %v\n%s", err, buf.String())
	}
	if payload.WouldCallProvider || !payload.WouldPostComment || !payload.MissingPostTarget {
		t.Fatalf("expected missing post target in payload: %+v", payload)
	}
}

func TestRootDryRunUpdateMetadataReportsMissingTarget(t *testing.T) {
	var buf strings.Builder
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--dry-run", "--update-title", "--update-description", "--format=json", "--file=testdata/simple.diff", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("dry-run update metadata without --pr should not fail: %v", err)
	}
	var payload struct {
		WouldCallProvider   bool `json:"would_call_provider"`
		WouldUpdateTitle    bool `json:"would_update_title"`
		WouldUpdateBody     bool `json:"would_update_description"`
		MissingUpdateTarget bool `json:"missing_update_target"`
	}
	if err := json.Unmarshal([]byte(buf.String()), &payload); err != nil {
		t.Fatalf("invalid dry-run json: %v\n%s", err, buf.String())
	}
	if payload.WouldCallProvider || !payload.WouldUpdateTitle || !payload.WouldUpdateBody || !payload.MissingUpdateTarget {
		t.Fatalf("expected missing update target in payload: %+v", payload)
	}
}

func TestUpdateMetadataFlagsValidateUsage(t *testing.T) {
	for _, args := range [][]string{
		{"--update-title", "--file=testdata/simple.diff", "--provider=openai"},
		{"--update-description", "--commit-msg", "--pr=https://github.com/o/r/pull/1", "--provider=openai"},
		{"--update-description", "--title-only", "--pr=https://github.com/o/r/pull/1", "--provider=openai"},
	} {
		cmd := newRootCmd(dummyChatFn)
		cmd.SetArgs(args)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		err := cmd.Execute()
		if err == nil {
			t.Fatalf("expected invalid usage for args %v", args)
		}
		var coded interface{ ExitCode() int }
		if !errors.As(err, &coded) || coded.ExitCode() != 4 {
			t.Fatalf("expected invalid usage exit code 4, got %T %v", err, err)
		}
	}
}

func TestMetadataOnlyOutputWritesFileAndSuppressesStdout(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "summary.json")
	var stdout strings.Builder
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--summary-only", "--format=json", "--file=testdata/simple.diff", "--provider=openai", "--output=" + outFile})
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("summary-only output failed: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected stdout suppressed when output file is set, got %q", stdout.String())
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("expected summary output file: %v", err)
	}
	var summary diffSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatalf("invalid summary json: %v\n%s", err, string(data))
	}
	if summary.FileCount == 0 {
		t.Fatalf("expected file summary, got %+v", summary)
	}
}

func TestMetadataOnlyRejectsPostAndClipboard(t *testing.T) {
	for _, args := range [][]string{
		{"--changed-files", "--post", "--pr=https://github.com/o/r/pull/1", "--file=testdata/simple.diff", "--provider=openai"},
		{"--summary-only", "--clipboard=all", "--file=testdata/simple.diff", "--provider=openai"},
	} {
		cmd := newRootCmd(dummyChatFn)
		cmd.SetArgs(args)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		err := cmd.Execute()
		if err == nil {
			t.Fatalf("expected metadata-only side effect rejection for args %v", args)
		}
		var coded interface{ ExitCode() int }
		if !errors.As(err, &coded) || coded.ExitCode() != 4 {
			t.Fatalf("expected invalid usage exit code 4, got %T %v", err, err)
		}
	}
}

func TestRootDryRunRejectsOtherMetadataOnlyModes(t *testing.T) {
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"--dry-run", "--debug", "--file=testdata/simple.diff", "--provider=openai"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected --dry-run --debug error")
	}
	if !strings.Contains(err.Error(), "--dry-run cannot be combined") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChangelogPresetAndDryRunConflictExitCodes(t *testing.T) {
	for _, args := range [][]string{
		{"changelog", "--preset=bad", "--dry-run", "--file=testdata/simple.diff", "--provider=openai"},
		{"changelog", "--dry-run", "--estimate", "--file=testdata/simple.diff", "--provider=openai"},
	} {
		cmd := newRootCmd(dummyChatFn)
		cmd.SetArgs(args)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		err := cmd.Execute()
		if err == nil {
			t.Fatalf("expected changelog invalid usage for args %v", args)
		}
		var coded interface{ ExitCode() int }
		if !errors.As(err, &coded) || coded.ExitCode() != 4 {
			t.Fatalf("expected invalid usage exit code 4, got %T %v", err, err)
		}
	}
}

func TestDoctorCIPresetDefaultsToJSON(t *testing.T) {
	var buf strings.Builder
	cmd := newRootCmd(dummyChatFn)
	cmd.SetArgs([]string{"doctor", "--preset=ci", "--provider=openai"})
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor ci preset failed: %v", err)
	}
	var payload struct {
		Preset string `json:"preset"`
	}
	if err := json.Unmarshal([]byte(buf.String()), &payload); err != nil {
		t.Fatalf("expected doctor ci preset to default to json, got %q: %v", buf.String(), err)
	}
	if payload.Preset != "ci" {
		t.Fatalf("expected ci preset in payload, got %+v", payload)
	}
}

func TestFlagCompletionsIncludePresetsAndProviders(t *testing.T) {
	cmd := newRootCmd(dummyChatFn)
	presetCompletion, ok := cmd.GetFlagCompletionFunc("preset")
	if !ok {
		t.Fatal("missing preset completion")
	}
	presets, directive := presetCompletion(cmd, nil, "lo")
	if directive == 0 || !containsString(presets, "local-fast") {
		t.Fatalf("expected local-fast completion, got %v directive=%v", presets, directive)
	}

	providerCompletion, ok := cmd.GetFlagCompletionFunc("provider")
	if !ok {
		t.Fatal("missing provider completion")
	}
	providers, _ := providerCompletion(cmd, nil, "cod")
	if !containsString(providers, "codex-cli") {
		t.Fatalf("expected codex-cli completion, got %v", providers)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
