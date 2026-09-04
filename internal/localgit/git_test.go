package localgit

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func TestQuickCommitDryRunDiffIncludesUntrackedFiles(t *testing.T) {
	dir := withGitRepo(t)
	chdir(t, dir)

	writeFile(t, dir, "new.txt", "new\n")

	diff, err := QuickCommitDryRunDiff(false)
	if err != nil {
		t.Fatalf("QuickCommitDryRunDiff failed: %v", err)
	}
	if !strings.Contains(diff, "diff --git a/new.txt b/new.txt") || !strings.Contains(diff, "+new") {
		t.Fatalf("expected untracked file diff, got:\n%s", diff)
	}

	trackedOnlyDiff, err := QuickCommitDryRunDiff(true)
	if err != nil {
		t.Fatalf("QuickCommitDryRunDiff tracked-only failed: %v", err)
	}
	if strings.Contains(trackedOnlyDiff, "new.txt") {
		t.Fatalf("tracked-only dry-run diff included untracked file:\n%s", trackedOnlyDiff)
	}
}

func TestQuickCommitDryRunDiffExcludesCommittedBranchHistory(t *testing.T) {
	dir := withGitRepo(t)
	chdir(t, dir)

	writeFile(t, dir, "committed.txt", "base\n")
	writeFile(t, dir, "pending.txt", "base\n")
	if err := Add(); err != nil {
		t.Fatal(err)
	}
	if err := CommitMessage("feat: initial"); err != nil {
		t.Fatal(err)
	}
	base := runGit(t, dir, "rev-parse", "HEAD")
	runGit(t, dir, "checkout", "-b", "feature")

	writeFile(t, dir, "committed.txt", "branch history\n")
	if err := Add(); err != nil {
		t.Fatal(err)
	}
	if err := CommitMessage("feat: committed branch change"); err != nil {
		t.Fatal(err)
	}
	// Reproduce the default root-diff environment where origin/main points to
	// the branch base. A quick-commit preview must not use this merge base.
	runGit(t, dir, "update-ref", "refs/remotes/origin/main", base)

	writeFile(t, dir, "pending.txt", "pending work\n")
	diff, err := QuickCommitDryRunDiff(true)
	if err != nil {
		t.Fatalf("QuickCommitDryRunDiff failed: %v", err)
	}
	if !strings.Contains(diff, "pending.txt") || !strings.Contains(diff, "+pending work") {
		t.Fatalf("pending change missing from preview:\n%s", diff)
	}
	if strings.Contains(diff, "committed.txt") || strings.Contains(diff, "branch history") {
		t.Fatalf("preview included already committed branch history:\n%s", diff)
	}
}

func TestDiffRejectsOptionLikeRevision(t *testing.T) {
	tests := []string{
		"--stat",
		"HEAD..--stat",
		"main...--stat",
	}
	for _, commit := range tests {
		t.Run(commit, func(t *testing.T) {
			_, err := Diff(commit, false, nil)
			if err == nil || !strings.Contains(err.Error(), "must not start with '-'") {
				t.Fatalf("expected option-like revision error, got %v", err)
			}
		})
	}
}

func TestLocalGitDiffExplicitRevisionRangeAndExclude(t *testing.T) {
	dir := withGitRepo(t)
	chdir(t, dir)

	writeFile(t, dir, "keep.txt", "one\n")
	writeFile(t, dir, "skip.txt", "one\n")
	if err := Add(); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := CommitMessage("feat: initial"); err != nil {
		t.Fatalf("CommitMessage failed: %v", err)
	}
	base := runGit(t, dir, "rev-parse", "HEAD")

	writeFile(t, dir, "keep.txt", "two\n")
	writeFile(t, dir, "skip.txt", "two\n")
	if err := Add(); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := CommitMessage("feat: update"); err != nil {
		t.Fatalf("CommitMessage failed: %v", err)
	}
	head := runGit(t, dir, "rev-parse", "HEAD")

	show, err := Diff(head, false, nil)
	if err != nil || !strings.Contains(show, "keep.txt") || !strings.Contains(show, "skip.txt") {
		t.Fatalf("Diff(head) = %q, %v", show, err)
	}
	ranged, err := Diff(base+".."+head, false, []string{"skip.txt"})
	if err != nil {
		t.Fatalf("Diff(range) failed: %v", err)
	}
	if !strings.Contains(ranged, "keep.txt") || strings.Contains(ranged, "skip.txt") {
		t.Fatalf("expected excluded path to be absent:\n%s", ranged)
	}
}

func TestCurrentBranchDetachedHead(t *testing.T) {
	dir := withGitRepo(t)
	chdir(t, dir)

	writeFile(t, dir, "file.txt", "one\n")
	if err := Add(); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := CommitMessage("feat: initial"); err != nil {
		t.Fatalf("CommitMessage failed: %v", err)
	}
	runGit(t, dir, "checkout", "--detach", "HEAD")

	branch, err := CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch failed: %v", err)
	}
	if branch != "" {
		t.Fatalf("expected detached HEAD branch to be empty, got %q", branch)
	}
}

func TestFormatUntrackedFileDiffVariants(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	if err := os.Mkdir("dir", 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := formatUntrackedFileDiff("dir"); err != nil || got != "" {
		t.Fatalf("directory diff = %q, %v", got, err)
	}

	writeFile(t, dir, "empty.txt", "")
	if got, err := formatUntrackedFileDiff("empty.txt"); err != nil || !strings.Contains(got, "+++ b/empty.txt") {
		t.Fatalf("empty file diff = %q, %v", got, err)
	}

	writeFile(t, dir, "no-newline.txt", "one")
	if got, err := formatUntrackedFileDiff("no-newline.txt"); err != nil || !strings.Contains(got, "\\ No newline at end of file") {
		t.Fatalf("no-newline diff = %q, %v", got, err)
	}

	writeFile(t, dir, "binary.bin", "a\x00b")
	if got, err := formatUntrackedFileDiff("binary.bin"); err != nil || !strings.Contains(got, "Binary files /dev/null") {
		t.Fatalf("binary diff = %q, %v", got, err)
	}

	writeFile(t, dir, "script.sh", "#!/bin/sh\n")
	if runtime.GOOS != "windows" {
		if err := os.Chmod(filepath.Join(dir, "script.sh"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := formatUntrackedFileDiff("script.sh"); err != nil || !strings.Contains(got, "new file mode 100755") {
		t.Fatalf("executable diff = %q, %v", got, err)
	}

	if runtime.GOOS != "windows" {
		if err := os.Symlink("empty.txt", filepath.Join(dir, "link.txt")); err != nil {
			t.Fatal(err)
		}
		if got, err := formatUntrackedFileDiff("link.txt"); err != nil || !strings.Contains(got, "new file mode 120000") || !strings.Contains(got, "+empty.txt") {
			t.Fatalf("symlink diff = %q, %v", got, err)
		}
	}
}

func TestFormatDiffPathQuotesSpecialCharacters(t *testing.T) {
	got := formatDiffPath("a/", "space name.txt")
	if !strings.HasPrefix(got, `"a/space name.txt"`) {
		t.Fatalf("expected quoted diff path, got %q", got)
	}
}

func TestPushFailureIncludesGitOutput(t *testing.T) {
	dir := withGitRepo(t)
	chdir(t, dir)
	runGit(t, dir, "remote", "add", "origin", filepath.Join(dir, "missing.git"))

	err := Push("main")
	if err == nil || !strings.Contains(err.Error(), "git push") {
		t.Fatalf("expected push failure, got %v", err)
	}
}
