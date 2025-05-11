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

func TestDetectGitHost_GitHub(t *testing.T) {
	orig := runGitRemote
	defer func() { runGitRemote = orig }()

	runGitRemote = func() ([]byte, error) {
		return []byte(`origin	git@github.com:user/repo.git (fetch)
origin	git@github.com:user/repo.git (push)`), nil
	}

	if got := detectGitHost(); got != GitHub {
		t.Fatalf("expected GitHub, got %v", got)
	}
}

func TestDetectGitHost_GitLab(t *testing.T) {
	orig := runGitRemote
	defer func() { runGitRemote = orig }()

	runGitRemote = func() ([]byte, error) {
		return []byte(`origin	https://gitlab.com/user/repo.git (fetch)
origin	https://gitlab.com/user/repo.git (push)`), nil
	}

	if got := detectGitHost(); got != GitLab {
		t.Fatalf("expected GitLab, got %v", got)
	}
}

func TestDetectGitHost_Unknown(t *testing.T) {
	orig := runGitRemote
	defer func() { runGitRemote = orig }()

	runGitRemote = func() ([]byte, error) {
		return []byte(`upstream	something-else.com (fetch)`), nil
	}

	if got := detectGitHost(); got != Unknown {
		t.Fatalf("expected Unknown, got %v", got)
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
