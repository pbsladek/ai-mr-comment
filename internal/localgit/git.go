package localgit

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RemoteURL returns the push URL for the origin remote.
func RemoteURL() (string, error) {
	out, err := exec.Command("git", "remote", "get-url", "origin").CombinedOutput() //nolint:gosec // G204: git is a fixed binary, "origin" is a constant.
	if err != nil {
		return "", fmt.Errorf("getting remote URL: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// AutoMergeBase returns the common ancestor commit between HEAD and the remote
// default branch, trying origin/main then origin/master.
func AutoMergeBase() (string, error) {
	for _, branch := range []string{"origin/main", "origin/master"} {
		out, err := exec.Command("git", "merge-base", "HEAD", branch).CombinedOutput() //nolint:gosec // G204: git is a fixed binary, args are internal constants.
		if err == nil {
			return strings.TrimSpace(string(out)), nil
		}
	}
	return "", fmt.Errorf("could not determine merge base: no origin/main or origin/master found")
}

// IsRepo reports whether the current directory is inside a git repository.
func IsRepo() bool {
	return exec.Command("git", "rev-parse", "--is-inside-work-tree").Run() == nil
}

// CurrentBranch returns the current branch name. Detached HEAD returns an
// empty branch and nil error.
func CurrentBranch() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput() //nolint:gosec // G204: git is a fixed binary, args are internal constants.
	if err != nil {
		out2, err2 := exec.Command("git", "symbolic-ref", "--short", "HEAD").CombinedOutput() //nolint:gosec // G204: git is a fixed binary, args are internal constants.
		if err2 != nil {
			return "", fmt.Errorf("getting current branch: %w", err)
		}
		return strings.TrimSpace(string(out2)), nil
	}
	branch := strings.TrimSpace(string(out))
	if branch == "HEAD" {
		return "", nil
	}
	return branch, nil
}

// Add stages all changes in the working tree.
func Add() error {
	out, err := exec.Command("git", "add", ".").CombinedOutput() //nolint:gosec // G204: git is a fixed binary, args are internal constants.
	if err != nil {
		return fmt.Errorf("git add: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// AddTracked stages only tracked modified/deleted files.
func AddTracked() error {
	out, err := exec.Command("git", "add", "-u").CombinedOutput() //nolint:gosec // G204: git is a fixed binary, args are internal constants.
	if err != nil {
		return fmt.Errorf("git add -u: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Commit creates a commit with a subject and optional body.
func Commit(message, body string) error {
	args := []string{"commit", "-m", message}
	if body != "" {
		args = append(args, "-m", body)
	}
	out, err := exec.Command("git", args...).CombinedOutput() //nolint:gosec // G204: git is a fixed binary, message/body are user-provided commit text.
	if err != nil {
		return fmt.Errorf("git commit: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// CommitMessage creates a commit from the supplied multi-line message.
func CommitMessage(message string) error {
	tmp, err := os.CreateTemp("", "ai-mr-comment-commit-*.txt")
	if err != nil {
		return fmt.Errorf("creating commit message file: %w", err)
	}
	name := tmp.Name()
	defer func() {
		_ = os.Remove(name)
	}()
	if _, err := tmp.WriteString(message); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing commit message file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing commit message file: %w", err)
	}
	out, err := exec.Command("git", "commit", "-F", name).CombinedOutput() //nolint:gosec // G204: git is a fixed binary, temp file path is created by this process.
	if err != nil {
		return fmt.Errorf("git commit: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ConfigValue(key string) (string, error) {
	out, err := exec.Command("git", "config", "--get", key).CombinedOutput() //nolint:gosec // G204: git is a fixed binary, key is caller-controlled from constants.
	if err != nil {
		return "", fmt.Errorf("git config %s: %w", key, err)
	}
	value := strings.TrimSpace(string(out))
	if value == "" {
		return "", fmt.Errorf("git config %s is empty", key)
	}
	return value, nil
}

func SignoffIdentity() (string, error) {
	name, err := ConfigValue("user.name")
	if err != nil {
		return "", err
	}
	email, err := ConfigValue("user.email")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s <%s>", name, email), nil
}

// Push pushes branch to origin and sets upstream tracking.
func Push(branch string) error {
	out, err := exec.Command("git", "push", "--set-upstream", "origin", branch).CombinedOutput() //nolint:gosec // G204: git is a fixed binary, branch is from CurrentBranch.
	if err != nil {
		return fmt.Errorf("git push: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// HasCommits reports whether the repository has at least one commit.
func HasCommits() bool {
	return exec.Command("git", "rev-parse", "HEAD").Run() == nil //nolint:gosec // G204: git is a fixed binary, args are internal constants.
}

// Diff returns a git diff for staged changes, an explicit commit/range, or the
// working tree against the merge base/HEAD. Patterns in exclude are passed as
// git pathspecs to filter files at the source.
func Diff(commit string, staged bool, exclude []string) (string, error) {
	var args []string
	if staged {
		args = []string{"diff", "--cached"}
	} else if commit != "" {
		if strings.Contains(commit, "..") {
			args = []string{"diff", commit}
		} else {
			args = []string{"show", "--format=", commit}
		}
	} else if base, err := AutoMergeBase(); err == nil {
		args = []string{"diff", base}
	} else if HasCommits() {
		args = []string{"diff", "HEAD"}
	} else {
		args = []string{"diff", "--cached"}
	}

	if len(exclude) > 0 {
		args = append(args, "--", ".")
		for _, pattern := range exclude {
			args = append(args, ":!"+pattern)
		}
	}

	out, err := exec.Command("git", args...).CombinedOutput() //nolint:gosec // G204: git is a fixed binary, args are controlled by internal logic.
	return string(out), err
}
