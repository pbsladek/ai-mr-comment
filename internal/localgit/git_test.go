package localgit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...) //nolint:gosec // Test helper invokes git with fixed test args.
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_AUTHOR_NAME=Test User",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test User",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func withGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.name", "Test User")
	runGit(t, dir, "config", "user.email", "test@example.com")
	return dir
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(orig)
	})
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLocalGitRepoBranchDiffAndCommit(t *testing.T) {
	dir := withGitRepo(t)
	chdir(t, dir)

	if !IsRepo() {
		t.Fatal("expected temp directory to be a git repo")
	}
	if HasCommits() {
		t.Fatal("expected no commits before first commit")
	}
	branch, err := CurrentBranch()
	if err != nil || branch != "main" {
		t.Fatalf("branch = %q, %v", branch, err)
	}

	writeFile(t, dir, "tracked.txt", "one\n")
	if err := Add(); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if diff, err := Diff("", true, nil); err != nil || !strings.Contains(diff, "+one") {
		t.Fatalf("staged diff = %q, %v", diff, err)
	}
	if err := CommitMessage("feat: initial"); err != nil {
		t.Fatalf("CommitMessage failed: %v", err)
	}
	if !HasCommits() {
		t.Fatal("expected commits after initial commit")
	}

	writeFile(t, dir, "tracked.txt", "two\n")
	if diff, err := Diff("", false, nil); err != nil || !strings.Contains(diff, "-one") || !strings.Contains(diff, "+two") {
		t.Fatalf("worktree diff = %q, %v", diff, err)
	}
	if err := AddTracked(); err != nil {
		t.Fatalf("AddTracked failed: %v", err)
	}
	if err := Commit("fix: update tracked", "body"); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
}

func TestLocalGitRemoteAndSignoff(t *testing.T) {
	dir := withGitRepo(t)
	chdir(t, dir)
	runGit(t, dir, "remote", "add", "origin", "git@example.com:owner/repo.git")

	remoteURL, err := RemoteURL()
	if err != nil || remoteURL != "git@example.com:owner/repo.git" {
		t.Fatalf("RemoteURL = %q, %v", remoteURL, err)
	}
	identity, err := SignoffIdentity()
	if err != nil || identity != "Test User <test@example.com>" {
		t.Fatalf("SignoffIdentity = %q, %v", identity, err)
	}
}
