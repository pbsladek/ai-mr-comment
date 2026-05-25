package app

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCommandInputHelpersEdgeCases(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("stdin diff"))
	if !commandStdinIsPiped(cmd) {
		t.Fatal("non-file stdin should be treated as piped")
	}
	got, err := readCommandInput(cmd, "-")
	if err != nil || got != "stdin diff" {
		t.Fatalf("read stdin = %q, %v", got, err)
	}

	missing := filepath.Join(t.TempDir(), "missing.diff")
	if _, err := readCommandInput(&cobra.Command{}, missing); err == nil {
		t.Fatal("expected missing file error")
	}

	errReader := errReadCloser{err: errors.New("read failed")}
	cmd = &cobra.Command{}
	cmd.SetIn(errReader)
	if _, err := readCommandInput(cmd, "-"); err == nil || !strings.Contains(err.Error(), "read failed") {
		t.Fatalf("expected read error, got %v", err)
	}
}

func TestDecodeAgentInputVariants(t *testing.T) {
	if _, err := decodeAgentInput("{bad"); err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("expected invalid JSON error, got %v", err)
	}
	if _, err := decodeAgentInput(`{"title":"T"}`); err == nil || !strings.Contains(err.Error(), "diff is required") {
		t.Fatalf("expected missing diff error, got %v", err)
	}
	got, err := decodeAgentInput(`{"branch":" feat/x ","title":" Title ","description":" Body ","diff":"diff --git a/a b/a\n+one"}`)
	if err != nil {
		t.Fatalf("decodeAgentInput failed: %v", err)
	}
	for _, want := range []string{"Branch: feat/x", "PR Title: Title", "PR Description: Body", "diff --git"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in decoded input:\n%s", want, got)
		}
	}
}

func TestEncodeJSONLineNilFields(t *testing.T) {
	var buf bytes.Buffer
	if err := encodeJSONLine(&buf, "token", nil); err != nil {
		t.Fatalf("encodeJSONLine failed: %v", err)
	}
	if !strings.Contains(buf.String(), `"type":"token"`) {
		t.Fatalf("json line = %s", buf.String())
	}
}

func TestSummarizeDiffAndRenderVariants(t *testing.T) {
	diff := strings.Join([]string{
		`diff --git "a/old name.go" "b/new name.go"`,
		"--- a/old name.go",
		"+++ b/new name.go",
		"-old",
		"+new",
		"diff --git a/image.bin b/image.bin",
		"Binary files /dev/null and b/image.bin differ",
		"diff --git a/patch.bin b/patch.bin",
		"GIT binary patch",
		"+ignored",
		"--- a/deleted.txt",
		"+++ /dev/null",
		"-deleted",
	}, "\n")
	summary := summarizeDiff(diff, "test", "model", true)
	if summary.FileCount < 3 || summary.Additions != 1 || summary.Deletions != 1 || !summary.Truncated {
		t.Fatalf("summary = %+v", summary)
	}
	foundBinary := false
	for _, file := range summary.Files {
		if file.Binary {
			foundBinary = true
		}
	}
	if !foundBinary {
		t.Fatalf("expected binary file in summary: %+v", summary.Files)
	}

	plain, err := renderDiffSummary(summary, "text", false)
	if err != nil || !strings.Contains(string(plain), "(binary)") {
		t.Fatalf("plain summary = %q, %v", plain, err)
	}
	filesJSON, err := renderDiffSummary(summary, "json", true)
	if err != nil || !strings.Contains(string(filesJSON), `"files"`) {
		t.Fatalf("files json = %q, %v", filesJSON, err)
	}
	fullJSON, err := renderDiffSummary(summary, "json", false)
	if err != nil || !strings.Contains(string(fullJSON), `"file_count"`) {
		t.Fatalf("full json = %q, %v", fullJSON, err)
	}

	okCmd := &cobra.Command{}
	okCmd.SetOut(io.Discard)
	if err := writeDiffSummary(okCmd, summary, "text", false); err != nil {
		t.Fatalf("writeDiffSummary stdout failed: %v", err)
	}
	cmd := &cobra.Command{}
	cmd.SetOut(testErrWriter{err: errors.New("write failed")})
	if err := writeDiffSummary(cmd, summary, "text", false); err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("expected writer error, got %v", err)
	}
	if err := writeDiffSummaryToFile(filepath.Join(t.TempDir(), "missing", "summary.txt"), summary, "text", false); err == nil {
		t.Fatal("expected writeDiffSummaryToFile error")
	}
}

func TestDiffPathParsingEdgeCases(t *testing.T) {
	if _, _, ok := parseDiffGitPaths("not a diff line"); ok {
		t.Fatal("expected non-diff line to fail")
	}
	if _, _, ok := parseDiffGitPaths("diff --git only-one"); ok {
		t.Fatal("expected missing second path to fail")
	}
	oldPath, newPath, ok := parseDiffGitPaths(`diff --git "a/space name.go" "b/new name.go"`)
	if !ok || oldPath != "space name.go" || newPath != "new name.go" {
		t.Fatalf("quoted paths = %q %q %v", oldPath, newPath, ok)
	}
	if token, rest, ok := nextDiffPathToken(`"unterminated`); ok || token != "" || rest != "" {
		t.Fatalf("unterminated token = %q %q %v", token, rest, ok)
	}
	if got := sanitizeRemoteURL("https://token@example.com/owner/repo.git"); got != "https://example.com/owner/repo.git" {
		t.Fatalf("sanitized URL = %q", got)
	}
	if got := sanitizeRemoteURL("not a url"); got != "not a url" {
		t.Fatalf("invalid sanitized URL = %q", got)
	}
}

type errReadCloser struct {
	err error
}

func (r errReadCloser) Read([]byte) (int, error) { return 0, r.err }

func (r errReadCloser) Close() error { return nil }

var _ io.ReadCloser = errReadCloser{}
