package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type GitHost string

const (
	GitHub  GitHost = "github"
	GitLab  GitHost = "gitlab"
	Unknown GitHost = "unknown"
)

func detectGitHost() GitHost {
	cmd := exec.Command("git", "remote", "-v")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return Unknown
	}
	output := string(out)
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "origin") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				url := parts[1]
				if strings.Contains(url, "github.com") {
					return GitHub
				} else if strings.Contains(url, "gitlab.com") {
					return GitLab
				}
			}
		}
	}
	return Unknown
}

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
	var filteredLines []string
	var newFiles, deletedFiles []string
	var currentFile string
	var inNew, inDelete bool

	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Binary files") {
			continue
		}
		if strings.HasPrefix(line, "diff --git") {
			if currentFile != "" {
				if inNew {
					newFiles = append(newFiles, currentFile)
				} else if inDelete {
					deletedFiles = append(deletedFiles, currentFile)
				}
			}
			inNew, inDelete = false, false
			parts := strings.Split(line, " ")
			if len(parts) > 2 {
				currentFile = strings.TrimPrefix(parts[2], "a/")
			}
			continue
		}
		if strings.HasPrefix(line, "+++ /dev/null") {
			inDelete = true
		} else if strings.HasPrefix(line, "--- /dev/null") {
			inNew = true
		}
		if !inNew && !inDelete {
			filteredLines = append(filteredLines, line)
		}
	}
	if currentFile != "" {
		if inNew {
			newFiles = append(newFiles, currentFile)
		} else if inDelete {
			deletedFiles = append(deletedFiles, currentFile)
		}
	}

	summary := ""
	if len(newFiles) > 0 {
		summary += "\nNew files:\n"
		for _, f := range newFiles {
			summary += "• " + f + "\n"
		}
	}
	if len(deletedFiles) > 0 {
		summary += "\nDeleted files:\n"
		for _, f := range deletedFiles {
			summary += "• " + f + "\n"
		}
	}

	truncated := truncateDiff(filteredLines, maxLines)
	return truncated + summary
}

func truncateDiff(lines []string, max int) string {
	if len(lines) <= max {
		return strings.Join(lines, "\n")
	}
	head := strings.Join(lines[:max/2], "\n")
	tail := strings.Join(lines[len(lines)-(max/2):], "\n")
	return head + "\n[...diff truncated...]\n" + tail
}
