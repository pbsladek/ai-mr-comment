package gitdiff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitByFile(t *testing.T) {
	raw := "diff --git a/foo.txt b/foo.txt\n+foo\n" +
		"diff --git a/bar.txt b/bar.txt\n+bar\n"

	chunks := SplitByFile(raw)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0], "foo.txt") || !strings.Contains(chunks[1], "bar.txt") {
		t.Fatalf("unexpected chunks: %#v", chunks)
	}
}

func TestSplitByFileEmpty(t *testing.T) {
	if got := SplitByFile(""); len(got) != 0 {
		t.Fatalf("expected no chunks, got %#v", got)
	}
}

func TestProcessTruncates(t *testing.T) {
	raw := strings.Join([]string{"1", "2", "3", "4", "5", "6"}, "\n")

	got := Process(raw, 4)
	if !strings.Contains(got, "[...diff truncated...]") {
		t.Fatalf("expected truncation marker, got %q", got)
	}
	if !strings.Contains(got, "1\n2") || !strings.Contains(got, "5\n6") {
		t.Fatalf("expected head and tail lines to be retained, got %q", got)
	}
}

func TestProcessNonPositiveLimitReturnsRaw(t *testing.T) {
	raw := "a\nb\nc"

	if got := Process(raw, 0); got != raw {
		t.Fatalf("expected raw diff, got %q", got)
	}
}

func TestReadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.diff")
	if err := os.WriteFile(path, []byte("diff --git a/x b/x\n+x\n"), 0o644); err != nil {
		t.Fatalf("failed to write diff: %v", err)
	}

	got, err := ReadFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "diff --git") {
		t.Fatalf("expected diff content, got %q", got)
	}
}
