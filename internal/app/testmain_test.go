package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var repoRoot string

func TestMain(m *testing.M) {
	root, err := findRepoRoot(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "find repo root: %v\n", err)
		os.Exit(1)
	}
	repoRoot = root
	if err := os.Chdir(repoRoot); err != nil {
		fmt.Fprintf(os.Stderr, "chdir repo root: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func findRepoRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s", start)
		}
		dir = parent
	}
}

func repoPath(t testing.TB, parts ...string) string {
	t.Helper()
	if repoRoot == "" {
		root, err := findRepoRoot(".")
		if err != nil {
			t.Fatal(err)
		}
		repoRoot = root
	}
	return filepath.Join(append([]string{repoRoot}, parts...)...)
}

func testdataPath(t testing.TB, name string) string {
	t.Helper()
	return repoPath(t, "testdata", name)
}

func readTestdata(t testing.TB, name string) string {
	t.Helper()
	content, err := os.ReadFile(testdataPath(t, name))
	if err != nil {
		t.Fatalf("read testdata/%s: %v", name, err)
	}
	return string(content)
}

func binaryNameForGOOS(name, goos string) string {
	if goos == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		return name + ".exe"
	}
	return name
}

func TestFindRepoRootWalksUpToGoMod(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "internal", "app")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := findRepoRoot(nested)
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Fatalf("expected %s, got %s", root, got)
	}
}

func TestRepoPathAndBinaryNameAreCrossPlatformSafe(t *testing.T) {
	diffPath := testdataPath(t, "simple.diff")
	if _, err := os.Stat(diffPath); err != nil {
		t.Fatalf("expected test fixture at %s: %v", diffPath, err)
	}
	if got := binaryNameForGOOS("ai-mr-comment", "windows"); got != "ai-mr-comment.exe" {
		t.Fatalf("windows binary name mismatch: %s", got)
	}
	if got := binaryNameForGOOS("ai-mr-comment.exe", "windows"); got != "ai-mr-comment.exe" {
		t.Fatalf("windows binary suffix duplicated: %s", got)
	}
	if got := binaryNameForGOOS("ai-mr-comment", "linux"); got != "ai-mr-comment" {
		t.Fatalf("linux binary name mismatch: %s", got)
	}
	if runtime.GOOS == "windows" && filepath.Separator != '\\' {
		t.Fatalf("unexpected filepath separator on windows: %q", filepath.Separator)
	}
}
