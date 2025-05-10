package main

import (
	"os"
	"strings"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	text := "This is a short sentence."
	tokens := estimateTokens(text)
	if tokens <= 0 {
		t.Errorf("Expected positive token count, got %d", tokens)
	}
}

func TestPromptTemplateGeneration(t *testing.T) {
	tt := []struct {
		host     GitHost
		expected string
	}{
		{GitHub, "GitHub PR comment"},
		{GitLab, "GitLab MR comment"},
		{Unknown, "MR/PR comment"},
	}
	for _, tc := range tt {
		pt := NewPromptTemplate(tc.host)
		if !strings.Contains(pt.Purpose, tc.expected) {
			t.Errorf("Prompt purpose mismatch: got %s", pt.Purpose)
		}
		if !strings.Contains(pt.Instructions, "Title:") {
			t.Error("Instructions should include formatting rules")
		}
	}
}

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

func TestDetectGitHost_Unknown(t *testing.T) {
	// Rename .git to simulate non-repo env
	_ = os.Rename(".git", ".git.bak")
	defer os.Rename(".git.bak", ".git")

	host := detectGitHost()
	if host != Unknown {
		t.Errorf("Expected Unknown host, got %s", host)
	}
}

func TestReadDiffFromFile(t *testing.T) {
	content := "diff --git a/x b/x\n+++ b/x\n"
	tmpFile := "tmp.diff"
	_ = os.WriteFile(tmpFile, []byte(content), 0644)
	defer os.Remove(tmpFile)

	data, err := readDiffFromFile(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(data, "+++ b/x") {
		t.Error("Expected diff content not found")
	}
}

func TestEstimateDebugOutput(t *testing.T) {
	text := strings.Repeat("x", 3500) // ~1000 tokens
	tokens := estimateTokens(text)
	if tokens < 900 || tokens > 1100 {
		t.Errorf("Unexpected token estimate: %d", tokens)
	}
}
