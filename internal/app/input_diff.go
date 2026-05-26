package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

type agentInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Diff        string `json:"diff"`
	Branch      string `json:"branch"`
}

func commandStdinIsPiped(cmd *cobra.Command) bool {
	in := cmd.InOrStdin()
	f, ok := in.(*os.File)
	if !ok {
		return true
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice == 0
}

func readCommandInput(cmd *cobra.Command, path string) (string, error) {
	if path != "" && path != "-" {
		return readDiffFromFile(path)
	}
	b, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func decodeAgentInput(raw string) (string, error) {
	var in agentInput
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return "", fmt.Errorf("invalid JSON input: %w", err)
	}
	if strings.TrimSpace(in.Diff) == "" {
		return "", errors.New("invalid JSON input: diff is required")
	}
	var sb strings.Builder
	if strings.TrimSpace(in.Branch) != "" {
		sb.WriteString("Branch: ")
		sb.WriteString(strings.TrimSpace(in.Branch))
		sb.WriteString("\n\n")
	}
	if strings.TrimSpace(in.Title) != "" {
		sb.WriteString("PR Title: ")
		sb.WriteString(strings.TrimSpace(in.Title))
		sb.WriteByte('\n')
	}
	if strings.TrimSpace(in.Description) != "" {
		sb.WriteString("PR Description: ")
		sb.WriteString(strings.TrimSpace(in.Description))
		sb.WriteString("\n\n")
	}
	sb.WriteString(in.Diff)
	return sb.String(), nil
}

func encodeJSONLine(w io.Writer, typ string, fields map[string]any) error {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["type"] = typ
	return json.NewEncoder(w).Encode(fields)
}

type diffFileSummary struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary,omitempty"`
}

type diffSummary struct {
	Source     string            `json:"source"`
	Files      []diffFileSummary `json:"files"`
	FileCount  int               `json:"file_count"`
	Additions  int               `json:"additions"`
	Deletions  int               `json:"deletions"`
	Lines      int               `json:"lines"`
	Bytes      int               `json:"bytes"`
	Truncated  bool              `json:"truncated"`
	TokenModel string            `json:"token_model,omitempty"`
}

func summarizeDiff(diffContent, source, model string, truncated bool) diffSummary {
	summary := diffSummary{
		Source:     source,
		Lines:      strings.Count(diffContent, "\n") + 1,
		Bytes:      len(diffContent),
		Truncated:  truncated,
		TokenModel: model,
	}
	current := -1
	oldPath := ""
	binaryPatch := false
	for _, line := range strings.Split(diffContent, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			if _, path, ok := parseDiffGitPaths(line); ok {
				summary.Files = append(summary.Files, diffFileSummary{Path: path})
				current = len(summary.Files) - 1
			}
			oldPath = ""
			binaryPatch = false
			continue
		}
		if strings.HasPrefix(line, "Binary files ") {
			if current >= 0 {
				summary.Files[current].Binary = true
			}
			continue
		}
		if strings.HasPrefix(line, "GIT binary patch") {
			if current >= 0 {
				summary.Files[current].Binary = true
			}
			binaryPatch = true
			continue
		}
		if binaryPatch {
			continue
		}
		if rawPath, ok := strings.CutPrefix(line, "--- "); ok {
			path := cleanDiffPath(rawPath)
			if path != "/dev/null" {
				oldPath = path
			}
			if current == -1 && path != "/dev/null" {
				summary.Files = append(summary.Files, diffFileSummary{Path: path})
				current = len(summary.Files) - 1
			}
			continue
		}
		if rawPath, ok := strings.CutPrefix(line, "+++ "); ok {
			path := cleanDiffPath(rawPath)
			if path == "/dev/null" {
				path = oldPath
			}
			if current == -1 {
				if path == "" {
					path = "/dev/null"
				}
				summary.Files = append(summary.Files, diffFileSummary{Path: path})
				current = len(summary.Files) - 1
				continue
			}
			if path != "" && path != "/dev/null" {
				summary.Files[current].Path = path
			} else if summary.Files[current].Path == "/dev/null" && oldPath != "" {
				summary.Files[current].Path = oldPath
			}
			continue
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++ ") {
			if current >= 0 {
				summary.Additions++
				summary.Files[current].Additions++
			}
			continue
		}
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "--- ") {
			if current >= 0 {
				summary.Deletions++
				summary.Files[current].Deletions++
			}
		}
	}
	if len(summary.Files) == 0 && strings.TrimSpace(diffContent) != "" {
		summary.Files = append(summary.Files, diffFileSummary{Path: "(input)"})
	}
	summary.FileCount = len(summary.Files)
	return summary
}

func parseDiffGitPaths(line string) (oldPath, newPath string, ok bool) {
	rest, ok := strings.CutPrefix(line, "diff --git ")
	if !ok {
		return "", "", false
	}
	rest = strings.TrimSpace(rest)
	first, rest, ok := nextDiffPathToken(rest)
	if !ok {
		return "", "", false
	}
	second, _, ok := nextDiffPathToken(rest)
	if !ok {
		return "", "", false
	}
	return cleanDiffPath(first), cleanDiffPath(second), true
}

func nextDiffPathToken(s string) (token, rest string, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", false
	}
	if s[0] != '"' {
		if strings.HasPrefix(s, "a/") {
			if idx := strings.LastIndex(s, " b/"); idx > 0 {
				return s[:idx], s[idx+1:], true
			}
		}
		if idx := strings.IndexByte(s, ' '); idx >= 0 {
			return s[:idx], s[idx+1:], true
		}
		return s, "", true
	}
	escaped := false
	for i := 1; i < len(s); i++ {
		switch {
		case escaped:
			escaped = false
		case s[i] == '\\':
			escaped = true
		case s[i] == '"':
			return s[:i+1], s[i+1:], true
		}
	}
	return "", "", false
}

func cleanDiffPath(path string) string {
	path = strings.TrimSpace(path)
	if unquoted, err := strconv.Unquote(path); err == nil {
		path = unquoted
	}
	if stripped, ok := strings.CutPrefix(path, "a/"); ok {
		path = stripped
	} else if stripped, ok := strings.CutPrefix(path, "b/"); ok {
		path = stripped
	}
	return path
}

func sanitizeRemoteURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = nil
	return u.String()
}

func writeDiffSummary(cmd *cobra.Command, summary diffSummary, format string, changedFilesOnly bool) error {
	out := cmd.OutOrStdout()
	content, err := renderDiffSummary(summary, format, changedFilesOnly)
	if err != nil {
		return err
	}
	_, err = out.Write(content)
	return err
}

func renderDiffSummary(summary diffSummary, format string, changedFilesOnly bool) ([]byte, error) {
	var buf bytes.Buffer
	if format == "json" {
		if changedFilesOnly {
			files := make([]string, 0, len(summary.Files))
			for _, f := range summary.Files {
				files = append(files, f.Path)
			}
			err := json.NewEncoder(&buf).Encode(struct {
				Source string   `json:"source"`
				Files  []string `json:"files"`
			}{Source: summary.Source, Files: files})
			return buf.Bytes(), err
		}
		err := json.NewEncoder(&buf).Encode(summary)
		return buf.Bytes(), err
	}
	if changedFilesOnly {
		for _, f := range summary.Files {
			_, _ = fmt.Fprintln(&buf, f.Path)
		}
		return buf.Bytes(), nil
	}
	_, _ = fmt.Fprintf(&buf, "Diff summary\n")
	_, _ = fmt.Fprintf(&buf, "- Source: %s\n", summary.Source)
	_, _ = fmt.Fprintf(&buf, "- Files: %d\n", summary.FileCount)
	_, _ = fmt.Fprintf(&buf, "- Lines: %d\n", summary.Lines)
	_, _ = fmt.Fprintf(&buf, "- Bytes: %d\n", summary.Bytes)
	_, _ = fmt.Fprintf(&buf, "- Additions: %d\n", summary.Additions)
	_, _ = fmt.Fprintf(&buf, "- Deletions: %d\n", summary.Deletions)
	_, _ = fmt.Fprintf(&buf, "- Truncated: %v\n", summary.Truncated)
	_, _ = fmt.Fprintln(&buf, "\nChanged files:")
	for _, f := range summary.Files {
		if f.Binary {
			_, _ = fmt.Fprintf(&buf, "- %s (binary)\n", f.Path)
		} else {
			_, _ = fmt.Fprintf(&buf, "- %s (+%d/-%d)\n", f.Path, f.Additions, f.Deletions)
		}
	}
	return buf.Bytes(), nil
}

func writeDiffSummaryToFile(path string, summary diffSummary, format string, changedFilesOnly bool) error {
	content, err := renderDiffSummary(summary, format, changedFilesOnly)
	if err != nil {
		return err
	}
	return os.WriteFile(path, content, 0600)
}
