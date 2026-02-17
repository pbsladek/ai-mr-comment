package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func getGitDiff(commit string) (string, error) {
	var cmd *exec.Cmd
	if commit != "" {
		if strings.Contains(commit, "..") {
			cmd = exec.Command("git", "diff", commit)
		} else {
			cmd = exec.Command("git", "diff", fmt.Sprintf("%s^", commit), commit)
		}
	} else {
		cmd = exec.Command("git", "diff")
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func readDiffFromFile(path string) (string, error) {
	bytes, err := os.ReadFile(path)
	return string(bytes), err
}

func processDiff(raw string, maxLines int) string {
	lines := strings.Split(raw, "\n")
	return truncateDiff(lines, maxLines)
}

func truncateDiff(lines []string, max int) string {
	if len(lines) <= max {
		return strings.Join(lines, "\n")
	}
	head := strings.Join(lines[:max/2], "\n")
	tail := strings.Join(lines[len(lines)-(max/2):], "\n")
	return head + "\n[...diff truncated...]\n" + tail
}
