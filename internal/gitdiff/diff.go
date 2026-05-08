package gitdiff

import (
	"os"
	"strings"
)

// ReadFile reads a raw diff from the given file path.
func ReadFile(path string) (string, error) {
	bytes, err := os.ReadFile(path) //nolint:gosec // G304: reading user-supplied diff file is intentional
	return string(bytes), err
}

// SplitByFile splits a raw git diff into per-file chunks.
// Each chunk starts with a "diff --git" header and includes all hunks for that file.
func SplitByFile(raw string) []string {
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

// Process truncates the raw diff to at most maxLines lines to avoid exceeding
// provider context limits.
func Process(raw string, maxLines int) string {
	lines := strings.Split(raw, "\n")
	return Truncate(lines, maxLines)
}

// Truncate keeps the first and last halves of lines when the total exceeds max,
// inserting a marker at the cut point.
func Truncate(lines []string, max int) string {
	if max <= 0 {
		return strings.Join(lines, "\n")
	}
	if len(lines) <= max {
		return strings.Join(lines, "\n")
	}
	head := strings.Join(lines[:max/2], "\n")
	tail := strings.Join(lines[len(lines)-(max/2):], "\n")
	return head + "\n[...diff truncated...]\n" + tail
}
