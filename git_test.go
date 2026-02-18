package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestProcessDiffAndTruncate(t *testing.T) {
	raw := `
diff --git a/foo.txt b/foo.txt
index e69de29..4b825dc 100644
--- a/foo.txt
+++ b/foo.txt
@@ -0,0 +1,2 @@
+Hello
+World
`
	output := processDiff(raw, 10)
	if !strings.Contains(output, "Hello") || !strings.Contains(output, "World") {
		t.Error("Diff output missing expected content")
	}
}

func TestProcessDiff_Truncation(t *testing.T) {
	lines := []string{}
	for i := 0; i < 20; i++ {
		lines = append(lines, "line")
	}
	raw := strings.Join(lines, "\n")

	// Max 10 lines
	output := processDiff(raw, 10)

	if !strings.Contains(output, "[...diff truncated...]") {
		t.Error("Expected truncation message")
	}
	if len(strings.Split(output, "\n")) > 15 {
		t.Errorf("Output too long: %d lines", len(strings.Split(output, "\n")))
	}
}

func TestGetGitDiff_NoArgs(t *testing.T) {
	// We're in a git repo, so this should not error
	_, err := getGitDiff("", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetGitDiff_WithCommit(t *testing.T) {
	// Skip if HEAD has no parent (shallow clone or single-commit repo)
	if err := exec.Command("git", "rev-parse", "HEAD^").Run(); err != nil {
		t.Skip("skipping: HEAD has no parent commit")
	}
	result, err := getGitDiff("HEAD", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result
}

func TestGetGitDiff_WithRange(t *testing.T) {
	// Skip if HEAD~1 doesn't exist (shallow clone or single-commit repo)
	if err := exec.Command("git", "rev-parse", "HEAD~1").Run(); err != nil {
		t.Skip("skipping: HEAD~1 does not exist")
	}
	result, err := getGitDiff("HEAD~1..HEAD", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result
}

func TestGetGitDiff_Staged(t *testing.T) {
	result, err := getGitDiff("", true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result
}

func TestGetGitDiff_Exclude(t *testing.T) {
	result, err := getGitDiff("", false, []string{"*.md"})
	if err != nil {
		t.Fatalf("unexpected error with exclude: %v", err)
	}
	_ = result
}

func TestGetAutoMergeBase(t *testing.T) {
	base, err := getAutoMergeBase()
	if err != nil {
		t.Skip("skipping: no origin/main or origin/master remote found")
	}
	if base == "" {
		t.Error("expected non-empty merge base")
	}
}

func TestGetGitDiff_AutoBase(t *testing.T) {
	if _, err := getAutoMergeBase(); err != nil {
		t.Skip("skipping: no remote found for auto base-branch detection")
	}
	result, err := getGitDiff("", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result
}

func TestSplitDiffByFile_Empty(t *testing.T) {
	chunks := splitDiffByFile("")
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty diff, got %d", len(chunks))
	}
}

func TestSplitDiffByFile_Single(t *testing.T) {
	raw := "diff --git a/foo.txt b/foo.txt\n--- a/foo.txt\n+++ b/foo.txt\n@@ -1 +1 @@\n-old\n+new\n"
	chunks := splitDiffByFile(raw)
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0], "diff --git a/foo.txt") {
		t.Error("expected chunk to contain diff header")
	}
}

func TestSplitDiffByFile_Multi(t *testing.T) {
	raw := "diff --git a/foo.txt b/foo.txt\n+foo\n" +
		"diff --git a/bar.txt b/bar.txt\n+bar\n"
	chunks := splitDiffByFile(raw)
	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0], "foo.txt") {
		t.Error("first chunk should contain foo.txt")
	}
	if !strings.Contains(chunks[1], "bar.txt") {
		t.Error("second chunk should contain bar.txt")
	}
}

func TestReadDiffFromFile(t *testing.T) {
	content := "diff --git a/x b/x\n+++ b/x\n"
	tmpFile := "tmp.diff"
	_ = os.WriteFile(tmpFile, []byte(content), 0644)
	defer func() { _ = os.Remove(tmpFile) }()

	data, err := readDiffFromFile(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(data, "+++ b/x") {
		t.Error("Expected diff content not found")
	}
}
