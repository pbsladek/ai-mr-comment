package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// getAutoMergeBase returns the common ancestor commit between HEAD and the
// remote default branch, trying origin/main then origin/master.
func getAutoMergeBase() (string, error) {
	for _, branch := range []string{"origin/main", "origin/master"} {
		out, err := exec.Command("git", "merge-base", "HEAD", branch).CombinedOutput()
		if err == nil {
			return strings.TrimSpace(string(out)), nil
		}
	}
	return "", fmt.Errorf("could not determine merge base: no origin/main or origin/master found")
}

// isGitRepo reports whether the current directory is inside a git repository.
func isGitRepo() bool {
	err := exec.Command("git", "rev-parse", "--is-inside-work-tree").Run()
	return err == nil
}

// getGitDiff returns the git diff for the given mode.
// Priority: staged > explicit commit > auto merge-base > unstaged working tree.
// Patterns in exclude are passed as git pathspecs (":!pattern") to filter files at the source.
func getGitDiff(commit string, staged bool, exclude []string) (string, error) {
	var args []string
	if staged {
		args = []string{"diff", "--cached"}
	} else if commit != "" {
		if strings.Contains(commit, "..") {
			args = []string{"diff", commit}
		} else {
			// Single commit: diff against its parent.
			args = []string{"diff", fmt.Sprintf("%s^", commit), commit}
		}
	} else if base, err := getAutoMergeBase(); err == nil {
		// Diff the merge base against the working tree (staged + unstaged).
		// This covers both committed and uncommitted changes on the branch.
		args = []string{"diff", base}
	} else {
		// No merge base found (no remote, detached HEAD, etc.).
		// Fall back to all changes relative to the last commit — includes both
		// staged and unstaged changes, so nothing is silently missed.
		args = []string{"diff", "HEAD"}
	}

	if len(exclude) > 0 {
		args = append(args, "--", ".")
		for _, pattern := range exclude {
			args = append(args, ":!"+pattern)
		}
	}

	out, err := exec.Command("git", args...).CombinedOutput()
	return string(out), err
}

// readDiffFromFile reads a raw diff from the given file path.
func readDiffFromFile(path string) (string, error) {
	bytes, err := os.ReadFile(path)
	return string(bytes), err
}

// splitDiffByFile splits a raw git diff into per-file chunks.
// Each chunk starts with a "diff --git" header and includes all hunks for that file.
func splitDiffByFile(raw string) []string {
	var chunks []string
	var current strings.Builder
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "diff --git") && current.Len() > 0 {
			chunks = append(chunks, current.String())
			current.Reset()
		}
		current.WriteString(line)
		current.WriteByte('\n')
	}
	if current.Len() > 0 && strings.TrimSpace(current.String()) != "" {
		chunks = append(chunks, current.String())
	}
	return chunks
}

// processDiff truncates the raw diff to at most maxLines lines to avoid
// exceeding provider context limits.
func processDiff(raw string, maxLines int) string {
	lines := strings.Split(raw, "\n")
	return truncateDiff(lines, maxLines)
}

// truncateDiff keeps the first and last halves of lines when the total exceeds
// max, inserting a marker at the cut point.
func truncateDiff(lines []string, max int) string {
	if len(lines) <= max {
		return strings.Join(lines, "\n")
	}
	head := strings.Join(lines[:max/2], "\n")
	tail := strings.Join(lines[len(lines)-(max/2):], "\n")
	return head + "\n[...diff truncated...]\n" + tail
}
