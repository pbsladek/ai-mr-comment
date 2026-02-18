package main

import (
	"os"
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
	_, err := getGitDiff("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetGitDiff_WithCommit(t *testing.T) {
	result, err := getGitDiff("HEAD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Result may be empty if HEAD has no parent diff, that's fine
	_ = result
}

func TestGetGitDiff_WithRange(t *testing.T) {
	result, err := getGitDiff("HEAD~1..HEAD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result
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
