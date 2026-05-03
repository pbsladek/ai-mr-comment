// Package main is the entry point for ai-mr-comment, a CLI tool that generates
// MR/PR comments from git diffs using AI providers.
package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/atotto/clipboard"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"golang.org/x/term"
)

// Version is set at build time via -ldflags "-X 'main.Version=...'"
// Falls back to VCS info embedded by the Go toolchain (go install / go build).
var Version = "dev"

// Commit is the short (7-char) commit SHA, set at build time via -ldflags "-X 'main.Commit=...'"
// Falls back to VCS info embedded by the Go toolchain (go install / go build).
var Commit = "unknown"

// CommitFull is the full commit SHA, set at build time via -ldflags "-X 'main.CommitFull=...'"
// Falls back to VCS info embedded by the Go toolchain (go install / go build).
var CommitFull = "unknown"

func init() {
	if Version != "dev" || Commit != "unknown" {
		return
	}
	// Attempt to read VCS metadata that `go build` embeds automatically (Go 1.18+).
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if len(s.Value) >= 7 {
					CommitFull = s.Value
					Commit = s.Value[:7]
				}
			case "vcs.version":
				if s.Value != "" {
					Version = s.Value
				}
			}
		}
	}
}

var debugWriterMu sync.Mutex

func main() {
	if err := newRootCmd(chatCompletions).Execute(); err != nil {
		var exitErr interface{ ExitCode() int }
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

type exitCodeError int

func (e exitCodeError) Error() string {
	return fmt.Sprintf("exit code %d", int(e))
}

func (e exitCodeError) ExitCode() int {
	return int(e)
}

type codedError struct {
	code int
	err  error
}

func (e codedError) Error() string {
	return e.err.Error()
}

func (e codedError) Unwrap() error {
	return e.err
}

func (e codedError) ExitCode() int {
	return e.code
}

func withExitCode(code int, err error) error {
	if err == nil {
		return nil
	}
	return codedError{code: code, err: err}
}

// parseVerdict extracts the VERDICT line prepended by the AI when --exit-code is
// active. Returns the verdict token ("PASS", "FAIL", or "UNKNOWN") and the body
// with the verdict line stripped. "UNKNOWN" indicates a missing/invalid verdict
// line and should be handled as fail-closed by callers.
func parseVerdict(comment string) (verdict, body string) {
	if verdictComment, ok := strings.CutPrefix(comment, "VERDICT: "); ok {
		lines := strings.SplitN(verdictComment, "\n", 2)
		verdict = strings.TrimSpace(lines[0])
		if len(lines) > 1 {
			body = strings.TrimSpace(lines[1])
		}
		return verdict, body
	}
	return "UNKNOWN", comment
}

// normalizeCommitMessage reduces model output to a single-line commit message.
// Some smaller models may return multiple lines or small preambles despite the prompt.
func normalizeCommitMessage(raw string) string {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	lines := strings.Split(normalized, "\n")
	candidates := make([]string, 0, len(lines))
	for _, line := range lines {
		clean := strings.TrimSpace(line)
		if clean == "" || strings.HasPrefix(clean, "```") {
			continue
		}

		// Strip common list markers and quote wrappers.
		if stripped, ok := strings.CutPrefix(clean, "- "); ok {
			clean = strings.TrimSpace(stripped)
		} else if stripped, ok := strings.CutPrefix(clean, "* "); ok {
			clean = strings.TrimSpace(stripped)
		} else if stripped, ok := strings.CutPrefix(clean, "+ "); ok {
			clean = strings.TrimSpace(stripped)
		}
		clean = strings.Trim(clean, "\"'`")

		// Strip common labels like "Commit message: ...".
		if idx := strings.Index(clean, ":"); idx > 0 {
			label := strings.ToLower(strings.TrimSpace(clean[:idx]))
			if label == "commit message" || label == "message" {
				clean = strings.TrimSpace(clean[idx+1:])
			}
		}

		clean = strings.Join(strings.Fields(clean), " ")
		if clean != "" {
			candidates = append(candidates, clean)
		}
	}

	if len(candidates) == 0 {
		return ""
	}
	for _, c := range candidates {
		if isConventionalCommitLine(c) {
			return c
		}
	}
	return candidates[0]
}

func isConventionalCommitLine(line string) bool {
	l := strings.ToLower(strings.TrimSpace(line))
	for _, typ := range conventionalCommitTypes {
		if strings.HasPrefix(l, typ+":") {
			return true
		}
		prefix := typ + "("
		if strings.HasPrefix(l, prefix) {
			rest := l[len(prefix):]
			if close := strings.Index(rest, ")"); close > 0 && close+1 < len(rest) && rest[close+1] == ':' {
				return true
			}
		}
	}
	return false
}

var conventionalCommitTypes = []string{"feat", "fix", "docs", "style", "refactor", "test", "chore", "perf", "ci", "build", "revert"}
var quickCommitMessageTemplateNames = []string{"short", "detailed", "release", "ticket"}

func isValidCommitType(typ string) bool {
	for _, valid := range conventionalCommitTypes {
		if typ == valid {
			return true
		}
	}
	return false
}

func isValidQuickCommitMessageTemplate(name string) bool {
	for _, valid := range quickCommitMessageTemplateNames {
		if name == valid {
			return true
		}
	}
	return false
}

func quickCommitMessageTemplateImpliesBody(name string) bool {
	switch name {
	case "detailed", "release", "ticket":
		return true
	default:
		return false
	}
}

func validateCommitScope(scope string) error {
	if scope == "" {
		return errors.New("--scope cannot be empty")
	}
	for _, r := range scope {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '.', '_', '-', '/':
			continue
		}
		return fmt.Errorf("--scope contains invalid character %q", r)
	}
	return nil
}

func parseConventionalSubject(subject string) (typ, scope, description string, breaking, ok bool) {
	head, desc, hasColon := strings.Cut(subject, ":")
	if !hasColon {
		return "", "", strings.TrimSpace(subject), false, false
	}
	head = strings.TrimSpace(head)
	desc = strings.TrimSpace(desc)
	if stripped, ok := strings.CutSuffix(head, "!"); ok {
		breaking = true
		head = stripped
	}
	if headWithoutClose, ok := strings.CutSuffix(head, ")"); ok {
		if open := strings.Index(headWithoutClose, "("); open > 0 {
			typ = headWithoutClose[:open]
			scope = headWithoutClose[open+1:]
		} else {
			typ = head
		}
	} else {
		typ = head
	}
	if stripped, ok := strings.CutSuffix(typ, "!"); ok {
		breaking = true
		typ = stripped
	}
	if !isValidCommitType(typ) {
		return "", "", strings.TrimSpace(subject), false, false
	}
	return typ, scope, desc, breaking, true
}

func applyCommitTypeScope(msg, forcedType, forcedScope string) string {
	if forcedType == "" && forcedScope == "" {
		return msg
	}
	subject, rest, hasRest := strings.Cut(msg, "\n")
	typ, scope, description, breaking, ok := parseConventionalSubject(strings.TrimSpace(subject))
	if !ok {
		typ = "feat"
		description = strings.TrimSpace(subject)
	}
	if forcedType != "" {
		typ = forcedType
	}
	if forcedScope != "" {
		scope = forcedScope
	}
	prefix := typ
	if breaking {
		prefix += "!"
	}
	if scope != "" {
		prefix += "(" + scope + ")"
	}
	subject = prefix + ": " + strings.TrimSpace(description)
	if hasRest {
		return subject + "\n" + rest
	}
	return subject
}

// commitTypeEmoji maps a conventional commit type to a trailing gitmoji.
// Breaking changes (type containing "!") map to 💥.
// Unknown types fall back to 🚀.
var commitTypeEmoji = map[string]string{
	"feat":     "✨",
	"fix":      "🐛",
	"docs":     "📝",
	"style":    "💄",
	"refactor": "♻️",
	"test":     "🧪",
	"chore":    "🔧",
	"perf":     "⚡",
	"ci":       "👷",
	"build":    "🏗️",
}

// appendCommitEmoji appends a type-matched gitmoji to the subject line of msg.
// The body (everything after the first newline) is left untouched.
// Breaking changes (subject contains "!") always get 💥.
func appendCommitEmoji(msg string) string {
	subject, rest, hasRest := strings.Cut(msg, "\n")
	emoji := "🚀"
	if strings.Contains(subject, "!") {
		emoji = "💥"
	} else {
		for t, e := range commitTypeEmoji {
			if strings.HasPrefix(subject, t+":") || strings.HasPrefix(subject, t+"(") {
				emoji = e
				break
			}
		}
	}
	subject = subject + " " + emoji
	if hasRest {
		return subject + "\n" + rest
	}
	return subject
}

// enforceBreakingChangeSubject rewrites a single-line conventional commit
// subject to include the breaking-change marker (e.g. "feat" → "feat!").
// If the subject already contains "!" it is returned unchanged.
// Non-conventional subjects are prefixed with "feat!: ".
func enforceBreakingChangeSubject(subject string) string {
	if strings.Contains(subject, "!") {
		return subject
	}
	types := []string{"feat", "fix", "chore", "refactor", "perf", "docs", "style", "test", "ci", "build"}
	for _, t := range types {
		// type(scope): description
		if strings.HasPrefix(subject, t+"(") {
			return t + "!" + subject[len(t):]
		}
		// type: description
		if strings.HasPrefix(subject, t+":") {
			return t + "!" + subject[len(t):]
		}
	}
	return "feat!: " + subject
}

// enforceBreakingChange ensures a commit message (single- or multi-line) uses
// the feat! type to signal a breaking change. Only the subject (first line) is
// rewritten; the body (everything after the first newline) is preserved as-is.
func enforceBreakingChange(msg string) string {
	subject, rest, hasRest := strings.Cut(msg, "\n")
	enforced := enforceBreakingChangeSubject(subject)
	if hasRest {
		return enforced + "\n" + rest
	}
	return enforced
}

// normalizeCommitBody lightly normalises a multi-line commit message returned
// by the AI when --multi-line is set. Unlike normalizeCommitMessage it does NOT
// collapse the output to a single line — the subject + body structure is kept.
// It strips surrounding whitespace, normalises line endings, unwraps a single
// fenced-code block if the model wrapped the whole output in one, and ensures
// the subject line is a valid conventional commit (prepending "feat: " if not).
func normalizeCommitBody(raw string) string {
	// Normalise line endings.
	out := strings.ReplaceAll(raw, "\r\n", "\n")
	out = strings.ReplaceAll(out, "\r", "\n")
	out = strings.TrimSpace(out)

	// Strip a surrounding fenced code block if the model wrapped the output.
	if strings.HasPrefix(out, "```") {
		lines := strings.Split(out, "\n")
		// Remove first line (``` or ```markdown etc.) and last line (```).
		if len(lines) >= 2 && strings.HasPrefix(lines[len(lines)-1], "```") {
			out = strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
		}
	}

	subject, rest, hasRest := strings.Cut(out, "\n")
	subject = strings.TrimSpace(subject)
	if hasRest {
		return subject + "\n" + rest
	}
	return subject
}

func longCommitBodyPromptSuffix(bodyLines int) string {
	if bodyLines <= 0 {
		bodyLines = 25
	}
	return fmt.Sprintf(`

Long-form body mode:
- Generate a detailed multi-line commit body suitable for a PR/MR description.
- Target approximately %d body lines after the subject and blank line.
- Include these sections when relevant: ## Summary, ## Changes, ## Rationale, ## Testing, ## Risks.
- Prefer concrete bullets that reference changed components, behavior, tests, migrations, config, and operational impact.
- Mention backward compatibility, rollout notes, follow-up work, and known limitations when the diff suggests them.
- Keep the subject under 72 characters; the longer detail belongs only in the body.`, bodyLines)
}

func appendCommitGuidance(prompt, commitType, commitScope, messageTemplate string) string {
	var hints []string
	if commitType != "" {
		hints = append(hints, fmt.Sprintf("- Use conventional commit type `%s`.", commitType))
	}
	if commitScope != "" {
		hints = append(hints, fmt.Sprintf("- Use conventional commit scope `%s`.", commitScope))
	}
	if messageTemplate != "" {
		hints = append(hints, quickCommitMessageTemplateGuidance(messageTemplate))
	}
	if len(hints) == 0 {
		return prompt
	}
	return prompt + "\n\nQuick-commit overrides:\n" + strings.Join(hints, "\n")
}

func quickCommitMessageTemplateGuidance(name string) string {
	switch name {
	case "short":
		return "- Use the `short` message template: generate one concise conventional commit subject only; no body unless another flag explicitly requires one."
	case "detailed":
		return "- Use the `detailed` message template: generate a subject plus a markdown body with ## Summary, ## Changes, ## Rationale, and ## Testing sections when relevant."
	case "release":
		return "- Use the `release` message template: generate a subject plus a release-note-ready body focused on user-visible behavior, compatibility, migration notes, risks, and validation."
	case "ticket":
		return "- Use the `ticket` message template: generate a subject plus a body that references the branch ticket key when present and includes issue context, implementation notes, and verification."
	default:
		return ""
	}
}

func appendSignedOffBy(message, identity string) string {
	trailer := "Signed-off-by: " + identity
	message = strings.TrimSpace(message)
	if message == "" {
		return trailer
	}
	if strings.Contains(message, trailer) {
		return message
	}
	return message + "\n\n" + trailer
}

func splitEditor(editor string) []string {
	return strings.Fields(editor)
}

func defaultEditor() string {
	for _, key := range []string{"GIT_EDITOR", "VISUAL", "EDITOR"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return "vi"
}

func editCommitMessage(message string) (string, error) {
	return editCommitMessageWithEditor(message, defaultEditor())
}

func editCommitMessageWithEditor(message, editor string) (string, error) {
	parts := splitEditor(editor)
	if len(parts) == 0 {
		return "", errors.New("--edit requires a non-empty editor command")
	}
	tmp, err := os.CreateTemp("", "ai-mr-comment-edit-*.txt")
	if err != nil {
		return "", fmt.Errorf("creating editor file: %w", err)
	}
	name := tmp.Name()
	defer func() {
		_ = os.Remove(name)
	}()
	if _, err := tmp.WriteString(strings.TrimSpace(message) + "\n"); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("writing editor file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("closing editor file: %w", err)
	}
	args := append(parts[1:], name)
	cmd := exec.Command(parts[0], args...) //nolint:gosec // G204: explicit editor command selected by the user via environment
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("running editor %q: %w", editor, err)
	}
	edited, err := os.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("reading edited commit message: %w", err)
	}
	out := strings.TrimSpace(string(edited))
	if out == "" {
		return "", errors.New("edited commit message is empty")
	}
	return out, nil
}

func stageQuickCommitChanges(trackedOnly bool) error {
	if trackedOnly {
		return gitAddTracked()
	}
	return gitAdd()
}

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
			summary.Additions++
			if current >= 0 {
				summary.Files[current].Additions++
			}
			continue
		}
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "--- ") {
			summary.Deletions++
			if current >= 0 {
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

var presetNames = []string{"ci", "local-fast", "security", "release-notes"}
var providerNames = []string{"openai", "anthropic", "gemini", "ollama", "claude-cli", "gemini-cli", "codex-cli"}
var templateNames = []string{"default", "conventional", "technical", "user-focused", "emoji", "sassy", "monday", "jira", "commit", "commit-emoji", "commit-conventional", "chaos", "haiku", "roast", "intern", "shakespeare", "manager", "yoda", "excuse"}

func completeValues(values []string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		matches := make([]string, 0, len(values))
		for _, value := range values {
			if strings.HasPrefix(value, toComplete) {
				matches = append(matches, value)
			}
		}
		return matches, cobra.ShellCompDirectiveNoFileComp
	}
}

func completeProfiles(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	profiles := listConfigProfiles()
	matches := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		if strings.HasPrefix(profile, toComplete) {
			matches = append(matches, profile)
		}
	}
	return matches, cobra.ShellCompDirectiveNoFileComp
}

func applyRootPreset(cmd *cobra.Command, name string, cfg *Config, format *string, exitCodeFlag, plain, generateTitle *bool) (promptSuffix string, err error) {
	switch name {
	case "":
		return "", nil
	case "ci":
		if !cmd.Flags().Changed("format") {
			*format = "json"
		}
		if !cmd.Flags().Changed("exit-code") {
			*exitCodeFlag = true
		}
		if !cmd.Flags().Changed("template") {
			cfg.Template = "technical"
		}
	case "local-fast":
		if !cmd.Flags().Changed("provider") {
			cfg.Provider = Ollama
		}
		if !cmd.Flags().Changed("model") {
			cfg.OllamaModel = "llama3.2"
		}
		if !cmd.Flags().Changed("plain") && !cmd.Flags().Changed("no-decorate") {
			*plain = true
		}
	case "security":
		if !cmd.Flags().Changed("template") {
			cfg.Template = "technical"
		}
		promptSuffix = "\n\nFocus especially on security vulnerabilities, unsafe defaults, credential exposure, injection risks, authorization bugs, and data handling issues."
	case "release-notes":
		if !cmd.Flags().Changed("template") {
			cfg.Template = "user-focused"
		}
		if !cmd.Flags().Changed("title") {
			*generateTitle = true
		}
	default:
		return "", fmt.Errorf("unknown preset %q: choose from %s", name, strings.Join(presetNames, ", "))
	}
	return promptSuffix, nil
}

func applyChangelogPreset(cmd *cobra.Command, name string, cfg *Config, format *string) (promptSuffix string, err error) {
	switch name {
	case "":
		return "", nil
	case "ci":
		if !cmd.Flags().Changed("format") {
			*format = "json"
		}
	case "local-fast":
		if !cmd.Flags().Changed("provider") {
			cfg.Provider = Ollama
		}
		if !cmd.Flags().Changed("model") {
			cfg.OllamaModel = "llama3.2"
		}
	case "security":
		promptSuffix = "\n\nCall out security-relevant changes, mitigations, and user-visible risk reductions clearly."
	case "release-notes":
		// The changelog command is already release-note oriented.
	default:
		return "", fmt.Errorf("unknown preset %q: choose from %s", name, strings.Join(presetNames, ", "))
	}
	return promptSuffix, nil
}

func isSupportedProvider(provider ApiProvider) bool {
	switch provider {
	case OpenAI, Anthropic, Gemini, Ollama, ClaudeCLI, GeminiCLI, CodexCLI:
		return true
	default:
		return false
	}
}

// newRootCmd builds the root cobra command, wiring flags to the provided chatFn.
// Accepting chatFn as a parameter allows tests to inject a mock without real API calls.
func newRootCmd(chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error)) *cobra.Command {
	var commit, diffFilePath, outputPath, provider, modelOverride, templateName, format, prURL, clipboardFlag, systemPromptFlag, profileName, presetName string
	var inputFormat, streamMode string
	var debug, staged, smartChunk, generateTitle, generateCommitMsg, multiLine, verbose, exitCodeFlag, postFlag, estimate, autoYes, versionFlag, dryRun bool
	var updateTitleFlag, updateDescriptionFlag bool
	var changedFilesOnly, summaryOnly bool
	var quiet, plain, printPrompt, printRequest, verdictOnly, titleOnly bool
	var mrChaos, mrHaiku, mrRoast bool
	var mrIntern, mrShakespeare, mrManager, mrYoda, mrExcuse bool
	var exclude []string

	rootCmd := &cobra.Command{
		Use:           "ai-mr-comment",
		Short:         "Generate MR/PR comments using AI",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if versionFlag {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "version=%s\ncommit=%s\ncommit_full=%s\nrepo=https://github.com/pbsladek/ai-mr-comment\n", Version, Commit, CommitFull)
				return nil
			}
			runStart := time.Now()
			cfg, err := loadConfigForProfile(profileName)
			if err != nil {
				return err
			}
			presetPromptSuffix, err := applyRootPreset(cmd, presetName, cfg, &format, &exitCodeFlag, &plain, &generateTitle)
			if err != nil {
				return withExitCode(4, err)
			}
			if cmd.Flags().Changed("provider") {
				cfg.Provider = ApiProvider(provider)
			}
			if cmd.Flags().Changed("model") {
				setModelOverride(cfg, modelOverride)
			}
			if cmd.Flags().Changed("template") {
				cfg.Template = templateName
			}
			if verbose {
				cfg.DebugWriter = cmd.ErrOrStderr()
				defer func() {
					debugLog(cfg, "total elapsed: %dms", time.Since(runStart).Milliseconds())
				}()
			}
			configFile := cfg.ConfigFile
			if configFile == "" {
				configFile = "(none)"
			}
			debugLog(cfg, "config: file=%s provider=%s model=%s template=%s", configFile, cfg.Provider, getModelName(cfg), cfg.Template)

			if !isSupportedProvider(cfg.Provider) {
				return errors.New("unsupported provider: " + string(cfg.Provider))
			}
			if postFlag && prURL == "" && !dryRun {
				return withExitCode(4, errors.New("--post requires --pr to specify a GitHub PR or GitLab MR URL"))
			}
			if (updateTitleFlag || updateDescriptionFlag) && prURL == "" && !dryRun {
				return withExitCode(4, errors.New("--update-title and --update-description require --pr to specify a GitHub PR or GitLab MR URL"))
			}
			if (updateTitleFlag || updateDescriptionFlag) && generateCommitMsg {
				return withExitCode(4, errors.New("--update-title and --update-description cannot be used with --commit-msg"))
			}
			if updateDescriptionFlag && titleOnly {
				return withExitCode(4, errors.New("--update-description cannot be used with --title-only"))
			}
			metadataOnly := dryRun || changedFilesOnly || summaryOnly || debug || printPrompt || printRequest
			if !metadataOnly {
				if cfgErr := validateProviderConfig(cfg); cfgErr != nil {
					return cfgErr
				}
			}
			if cancel := applyRequestTimeout(cmd, cfg); cancel != nil {
				defer cancel()
			}
			if quiet {
				format = "json"
			}
			if staged && commit != "" {
				return withExitCode(4, errors.New("--staged and --commit are mutually exclusive"))
			}
			if prURL != "" && (staged || commit != "" || diffFilePath != "") {
				return withExitCode(4, errors.New("--pr cannot be combined with --staged, --commit, or --file"))
			}
			if multiLine && !generateCommitMsg {
				return withExitCode(4, errors.New("--multi-line requires --commit-msg"))
			}
			effectiveTemplate := cfg.Template
			commitOnlyTemplates := map[string]bool{"commit": true, "commit-emoji": true, "commit-conventional": true}
			if commitOnlyTemplates[effectiveTemplate] && !generateCommitMsg {
				return fmt.Errorf("--template %s requires --commit-msg", effectiveTemplate)
			}
			mrOnlyTemplates := map[string]bool{
				"technical": true, "user-focused": true, "emoji": true, "sassy": true,
				"monday": true, "jira": true, "conventional": true,
				"chaos": true, "haiku": true, "roast": true, "intern": true,
				"shakespeare": true, "manager": true, "yoda": true, "excuse": true,
			}
			if mrOnlyTemplates[effectiveTemplate] && generateCommitMsg {
				return fmt.Errorf("--template %s cannot be combined with --commit-msg", effectiveTemplate)
			}
			if generateCommitMsg && generateTitle {
				return withExitCode(4, errors.New("--commit-msg and --title cannot be used together"))
			}
			if titleOnly && generateCommitMsg {
				return withExitCode(4, errors.New("--title-only cannot be used with --commit-msg"))
			}
			if exitCodeFlag && generateCommitMsg {
				return withExitCode(4, errors.New("--exit-code cannot be used with --commit-msg"))
			}
			if postFlag && prURL == "" && !dryRun {
				return withExitCode(4, errors.New("--post requires --pr to specify a GitHub PR or GitLab MR URL"))
			}
			if (updateTitleFlag || updateDescriptionFlag) && prURL == "" && !dryRun {
				return withExitCode(4, errors.New("--update-title and --update-description require --pr to specify a GitHub PR or GitLab MR URL"))
			}
			if (updateTitleFlag || updateDescriptionFlag) && generateCommitMsg {
				return withExitCode(4, errors.New("--update-title and --update-description cannot be used with --commit-msg"))
			}
			if updateDescriptionFlag && titleOnly {
				return withExitCode(4, errors.New("--update-description cannot be used with --title-only"))
			}
			if cmd.Flags().Changed("system-prompt") && cmd.Flags().Changed("template") {
				return withExitCode(4, errors.New("--system-prompt and --template are mutually exclusive"))
			}
			mrStyleFlags := []bool{mrChaos, mrHaiku, mrRoast, mrIntern, mrShakespeare, mrManager, mrYoda, mrExcuse}
			funStyleCount := 0
			for _, f := range mrStyleFlags {
				if f {
					funStyleCount++
				}
			}
			if funStyleCount > 1 {
				return withExitCode(4, errors.New("--chaos, --haiku, --roast, --intern, --shakespeare, --manager, --yoda, and --excuse are mutually exclusive"))
			}
			if funStyleCount > 0 && (cmd.Flags().Changed("template") || cmd.Flags().Changed("system-prompt")) {
				return withExitCode(4, errors.New("style flags cannot be combined with --template or --system-prompt"))
			}
			if funStyleCount > 0 && generateCommitMsg {
				return withExitCode(4, errors.New("style flags cannot be combined with --commit-msg"))
			}

			if format != "text" && format != "json" {
				return withExitCode(4, fmt.Errorf("unsupported format %q: must be text or json", format))
			}
			if inputFormat == "" {
				inputFormat = "text"
			}
			if inputFormat != "text" && inputFormat != "json" {
				return withExitCode(4, fmt.Errorf("unsupported input format %q: must be text or json", inputFormat))
			}
			if streamMode != "" && streamMode != "jsonl" {
				return withExitCode(4, fmt.Errorf("unsupported stream mode %q: must be jsonl", streamMode))
			}
			if streamMode == "jsonl" && outputPath != "" {
				return withExitCode(4, errors.New("--stream=jsonl cannot be combined with --output"))
			}
			if dryRun && streamMode == "jsonl" {
				return withExitCode(4, errors.New("--dry-run cannot be combined with --stream=jsonl"))
			}
			if dryRun && (debug || estimate || printPrompt || printRequest || changedFilesOnly || summaryOnly) {
				return withExitCode(4, errors.New("--dry-run cannot be combined with --debug, --estimate, --print-prompt, --print-request, --changed-files, or --summary-only"))
			}
			if (changedFilesOnly || summaryOnly) && (postFlag || clipboardFlag != "") {
				return withExitCode(4, errors.New("--changed-files and --summary-only cannot be combined with --post or --clipboard"))
			}

			var diffContent string
			var diffSource string
			diffFetchStart := time.Now()
			err = nil
			if inputFormat == "json" {
				if prURL != "" || staged || commit != "" {
					return withExitCode(4, errors.New("--input=json cannot be combined with --pr, --staged, or --commit"))
				}
				diffSource = "json"
				var rawInput string
				rawInput, err = readCommandInput(cmd, diffFilePath)
				if err == nil {
					diffContent, err = decodeAgentInput(rawInput)
				}
			} else if prURL != "" {
				switch {
				case isGitHubURL(prURL):
					diffSource = "github-pr: " + prURL
					diffContent, err = getPRDiff(cmd.Context(), prURL, cfg.GitHubToken, cfg.GitHubBaseURL)
				case isGitLabURL(prURL):
					diffSource = "gitlab-mr: " + prURL
					diffContent, err = getMRDiff(cmd.Context(), prURL, cfg.GitLabToken, cfg.GitLabBaseURL)
				default:
					return fmt.Errorf("unsupported URL %q: must be a GitHub PR (/pull/) or GitLab MR (/-/merge_requests/) URL", prURL)
				}
			} else if diffFilePath != "" {
				if diffFilePath == "-" {
					diffSource = "stdin"
				} else {
					diffSource = "file: " + diffFilePath
				}
				diffContent, err = readCommandInput(cmd, diffFilePath)
			} else if commandStdinIsPiped(cmd) {
				diffSource = "stdin"
				diffContent, err = readCommandInput(cmd, "-")
			} else {
				if !isGitRepo() {
					return fmt.Errorf("not a git repository. Run from inside a git repo or use --file to provide a diff")
				}
				switch {
				case staged:
					diffSource = "git (staged)"
				case commit != "":
					diffSource = "git (commit: " + commit + ")"
				default:
					diffSource = "git"
				}
				diffContent, err = getGitDiff(commit, staged, exclude)
			}
			debugLog(cfg, "diff fetch: elapsed=%dms", time.Since(diffFetchStart).Milliseconds())
			if err != nil {
				return err
			}
			if strings.TrimSpace(diffContent) == "" {
				if staged {
					return withExitCode(3, fmt.Errorf("no staged changes found. Stage your changes with 'git add' first"))
				}
				return withExitCode(3, fmt.Errorf("no diff found. Make sure you have uncommitted changes or specify a commit range with --commit"))
			}
			debugLog(cfg, "diff: source=%s bytes=%d", diffSource, len(diffContent))

			// Prepend the current branch name when diffing a local git repo.
			// This lets the AI and templates reference the branch/ticket number
			// (e.g. "feat/ABC-123-add-login") for linking in systems like Jira.
			// Skipped for --file and --pr since those have no local branch context.
			if prURL == "" && diffFilePath == "" && diffSource != "stdin" && diffSource != "json" {
				if branch, branchErr := getCurrentBranch(); branchErr == nil && branch != "" {
					diffContent = "Branch: " + branch + "\n\n" + diffContent
					debugLog(cfg, "branch: name=%s", branch)
				}
			}

			out := cmd.OutOrStdout()
			// When writing to a file, suppress all text output to the terminal.
			if outputPath != "" && !dryRun && !changedFilesOnly && !summaryOnly {
				out = io.Discard
			}
			// Summarize the raw diff before truncating so metadata-only modes do
			// not hide files that appear after the generation line limit.
			diffLines := strings.Split(diffContent, "\n")
			rawLines := len(diffLines)
			diffTruncated := rawLines > 4000
			summary := summarizeDiff(diffContent, diffSource, getModelName(cfg), diffTruncated)
			diffContent = truncateDiff(diffLines, 4000)
			debugLog(cfg, "diff: lines before truncation=%d after=%d (max=4000)", rawLines, strings.Count(diffContent, "\n")+1)

			if changedFilesOnly || summaryOnly {
				if outputPath != "" {
					return writeDiffSummaryToFile(outputPath, summary, format, changedFilesOnly && !summaryOnly)
				}
				return writeDiffSummary(cmd, summary, format, changedFilesOnly && !summaryOnly)
			}

			systemPrompt, templateErr := NewPromptTemplate(cfg.Template)
			if templateErr != nil {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Warning:", templateErr)
			}
			templateSource := "embedded"
			if cfg.Template != "default" {
				if templateErr == nil {
					templateSource = "filesystem"
				} else {
					templateSource = "embedded (fallback)"
				}
			}
			debugLog(cfg, "template: name=%q source=%s length=%d", cfg.Template, templateSource, len(systemPrompt))

			// --system-prompt overrides the template-derived prompt entirely.
			if systemPromptFlag != "" {
				override, spErr := resolveSystemPrompt(systemPromptFlag)
				if spErr != nil {
					return spErr
				}
				systemPrompt = override
				debugLog(cfg, "system-prompt: override applied length=%d", len(systemPrompt))
			}

			// Fun style flags override the system prompt with a personality template.
			switch {
			case mrChaos:
				systemPrompt = mrChaosPrompt
				debugLog(cfg, "style: chaos mode enabled")
			case mrHaiku:
				systemPrompt = mrHaikuPrompt
				debugLog(cfg, "style: haiku mode enabled")
			case mrRoast:
				systemPrompt = mrRoastPrompt
				debugLog(cfg, "style: roast mode enabled")
			case mrIntern:
				systemPrompt = mrInternPrompt
				debugLog(cfg, "style: intern mode enabled")
			case mrShakespeare:
				systemPrompt = mrShakespearePrompt
				debugLog(cfg, "style: shakespeare mode enabled")
			case mrManager:
				systemPrompt = mrManagerPrompt
				debugLog(cfg, "style: manager mode enabled")
			case mrYoda:
				systemPrompt = mrYodaPrompt
				debugLog(cfg, "style: yoda mode enabled")
			case mrExcuse:
				systemPrompt = mrExcusePrompt
				debugLog(cfg, "style: excuse mode enabled")
			}
			if presetPromptSuffix != "" && systemPromptFlag == "" {
				systemPrompt += presetPromptSuffix
			}

			// When --exit-code is set, prepend a verdict instruction so the AI starts
			// its response with "VERDICT: PASS" or "VERDICT: FAIL".
			const exitCodePreamble = "Before your review, output a verdict on the very first line in exactly this format:\nVERDICT: PASS\nor\nVERDICT: FAIL\nUse FAIL if the diff contains critical bugs, security vulnerabilities, data loss risks, or broken public APIs. Use PASS for everything else. Then continue with your normal review on the next line.\n\n"
			if exitCodeFlag {
				systemPrompt = exitCodePreamble + systemPrompt
			}

			if printPrompt {
				_, _ = fmt.Fprint(out, systemPrompt)
				if !strings.HasSuffix(systemPrompt, "\n") {
					_, _ = fmt.Fprintln(out)
				}
				return nil
			}

			if printRequest {
				request := struct {
					Provider     string `json:"provider"`
					Model        string `json:"model"`
					SystemPrompt string `json:"system_prompt"`
					Diff         string `json:"diff"`
					DiffSource   string `json:"diff_source"`
					Template     string `json:"template"`
					Preset       string `json:"preset,omitempty"`
					Truncated    bool   `json:"truncated"`
				}{
					Provider:     string(cfg.Provider),
					Model:        getModelName(cfg),
					SystemPrompt: systemPrompt,
					Diff:         diffContent,
					DiffSource:   diffSource,
					Template:     cfg.Template,
					Preset:       presetName,
					Truncated:    diffTruncated,
				}
				return json.NewEncoder(out).Encode(request)
			}

			if dryRun {
				plan := struct {
					DryRun              bool        `json:"dry_run"`
					Provider            string      `json:"provider"`
					Model               string      `json:"model"`
					Template            string      `json:"template"`
					Preset              string      `json:"preset,omitempty"`
					DiffSource          string      `json:"diff_source"`
					Summary             diffSummary `json:"summary"`
					WouldCallProvider   bool        `json:"would_call_provider"`
					WouldWriteOutput    bool        `json:"would_write_output"`
					WouldCopyClipboard  bool        `json:"would_copy_clipboard"`
					WouldPostComment    bool        `json:"would_post_comment"`
					WouldUpdateTitle    bool        `json:"would_update_title"`
					WouldUpdateBody     bool        `json:"would_update_description"`
					MissingPostTarget   bool        `json:"missing_post_target,omitempty"`
					MissingUpdateTarget bool        `json:"missing_update_target,omitempty"`
					PostTarget          string      `json:"post_target,omitempty"`
				}{
					DryRun:              true,
					Provider:            string(cfg.Provider),
					Model:               getModelName(cfg),
					Template:            cfg.Template,
					Preset:              presetName,
					DiffSource:          diffSource,
					Summary:             summary,
					WouldCallProvider:   (!postFlag && !updateTitleFlag && !updateDescriptionFlag) || prURL != "",
					WouldWriteOutput:    outputPath != "",
					WouldCopyClipboard:  clipboardFlag != "",
					WouldPostComment:    postFlag,
					WouldUpdateTitle:    updateTitleFlag,
					WouldUpdateBody:     updateDescriptionFlag,
					MissingPostTarget:   postFlag && prURL == "",
					MissingUpdateTarget: (updateTitleFlag || updateDescriptionFlag) && prURL == "",
					PostTarget:          prURL,
				}
				if format == "json" {
					return json.NewEncoder(out).Encode(plan)
				}
				_, _ = fmt.Fprintln(out, "Dry run: no provider call, file write, clipboard write, PR/MR post, or PR/MR metadata update will be performed.")
				_, _ = fmt.Fprintf(out, "- Provider: %s\n", plan.Provider)
				_, _ = fmt.Fprintf(out, "- Model: %s\n", plan.Model)
				_, _ = fmt.Fprintf(out, "- Template: %s\n", plan.Template)
				if presetName != "" {
					_, _ = fmt.Fprintf(out, "- Preset: %s\n", presetName)
				}
				_, _ = fmt.Fprintf(out, "- Diff source: %s\n", diffSource)
				_, _ = fmt.Fprintf(out, "- Files: %d\n", summary.FileCount)
				_, _ = fmt.Fprintf(out, "- Additions: %d\n", summary.Additions)
				_, _ = fmt.Fprintf(out, "- Deletions: %d\n", summary.Deletions)
				if outputPath != "" {
					_, _ = fmt.Fprintf(out, "- Would write output: %s\n", outputPath)
				}
				if clipboardFlag != "" {
					_, _ = fmt.Fprintf(out, "- Would copy clipboard: %s\n", clipboardFlag)
				}
				if postFlag {
					if prURL == "" {
						_, _ = fmt.Fprintln(out, "- Would post comment: (missing --pr)")
					} else {
						_, _ = fmt.Fprintf(out, "- Would post comment: %s\n", prURL)
					}
				}
				if updateTitleFlag || updateDescriptionFlag {
					if prURL == "" {
						_, _ = fmt.Fprintln(out, "- Would update PR/MR metadata: (missing --pr)")
					} else {
						fields := []string{}
						if updateTitleFlag {
							fields = append(fields, "title")
						}
						if updateDescriptionFlag {
							fields = append(fields, "description")
						}
						_, _ = fmt.Fprintf(out, "- Would update PR/MR %s: %s\n", strings.Join(fields, "+"), prURL)
					}
				}
				return nil
			}

			if debug {
				showCostEstimate(cmd.Context(), cfg, systemPrompt, diffContent, out)
				return nil
			}

			if estimate {
				estimateOut := out
				if format == "json" {
					estimateOut = cmd.ErrOrStderr()
				}
				showCostEstimate(cmd.Context(), cfg, systemPrompt, diffContent, estimateOut)
				if !promptConfirm(cmd.ErrOrStderr(), os.Stdin, autoYes) {
					return nil
				}
			}

			// Stream tokens directly to the terminal when output is a real TTY,
			// text format is selected, smart-chunk is off, and no output file is set.
			// All other paths use the buffered chatFn to get the full response first.
			isTTY := fileIsTerminal(os.Stdout)
			shouldStream := isTTY && format == "text" && streamMode == "" && !smartChunk && outputPath == ""
			debugLog(cfg, "streaming: tty=%v format=%s smart-chunk=%v output-file=%q → enabled=%v",
				isTTY, format, smartChunk, outputPath, shouldStream)
			// streamedOK is set to true only when streaming completes successfully.
			// The output block uses it to decide whether body was already written.
			var streamedOK bool

			var comment string
			var title string
			var commitMessage string
			if titleOnly {
				title, err = timedCall(cfg, "title", func() (string, error) {
					return chatFn(cmd.Context(), cfg, cfg.Provider, titlePrompt, diffContent)
				})
				if err != nil {
					if cfg.Provider == Ollama && strings.Contains(err.Error(), "connection refused") {
						return fmt.Errorf("failed to connect to Ollama at %s.\nMake sure Ollama is running (try 'ollama serve') or check your configuration", cfg.OllamaEndpoint)
					}
					return err
				}
				title = strings.TrimSpace(title)
			} else if generateCommitMsg {
				// Skip description generation; produce only a commit message.
				debugLog(cfg, "commit-msg: generating commit message with separate API call (multi-line=%v)", multiLine)
				prompt := commitMsgPrompt
				if multiLine {
					prompt = commitMsgBodyPrompt
				} else if commitOnlyTemplates[effectiveTemplate] {
					prompt = systemPrompt
				}
				commitMessage, err = timedCall(cfg, "commit-msg", func() (string, error) {
					return chatFn(cmd.Context(), cfg, cfg.Provider, prompt, diffContent)
				})
				if err != nil {
					if cfg.Provider == Ollama && strings.Contains(err.Error(), "connection refused") {
						return fmt.Errorf("failed to connect to Ollama at %s.\nMake sure Ollama is running (try 'ollama serve') or check your configuration", cfg.OllamaEndpoint)
					}
					return err
				}
				if multiLine {
					commitMessage = normalizeCommitBody(commitMessage)
				} else {
					commitMessage = normalizeCommitMessage(commitMessage)
				}
			} else if smartChunk {
				chunks := splitDiffByFile(diffContent)
				debugLog(cfg, "smart-chunk: files=%d", len(chunks))
				if len(chunks) > 1 {
					// Summarize each file chunk independently in parallel, then do a final synthesis call.
					const chunkPrompt = "Summarize the changes in this file diff in 3-5 bullet points. Be concise and technical."
					summaries := make([]string, len(chunks))
					debugLog(cfg, "smart-chunk: summarizing %d chunks in parallel", len(chunks))
					eg, egCtx := errgroup.WithContext(cmd.Context())
					for i, chunk := range chunks {
						i, chunk := i, chunk // capture loop vars
						eg.Go(func() error {
							debugLog(cfg, "smart-chunk: processing chunk %d/%d", i+1, len(chunks))
							summary, chunkErr := timedCall(cfg, fmt.Sprintf("chunk-summary-%d", i+1), func() (string, error) {
								return chatFn(egCtx, cfg, cfg.Provider, chunkPrompt, processDiff(chunk, 1000))
							})
							if chunkErr != nil {
								return chunkErr
							}
							summaries[i] = summary
							return nil
						})
					}
					if chunkErr := eg.Wait(); chunkErr != nil {
						if cfg.Provider == Ollama && strings.Contains(chunkErr.Error(), "connection refused") {
							return fmt.Errorf("failed to connect to Ollama at %s.\nMake sure Ollama is running (try 'ollama serve') or check your configuration", cfg.OllamaEndpoint)
						}
						return chunkErr
					}
					debugLog(cfg, "smart-chunk: all chunks summarized, running synthesis call")
					combinedSummaries := strings.Join(summaries, "\n\n---\n\n")
					comment, err = timedCall(cfg, "synthesis", func() (string, error) {
						return chatFn(cmd.Context(), cfg, cfg.Provider, systemPrompt, combinedSummaries)
					})
				} else {
					comment, err = timedCall(cfg, "comment", func() (string, error) {
						return chatFn(cmd.Context(), cfg, cfg.Provider, systemPrompt, diffContent)
					})
				}
			} else if shouldStream {
				comment, err = timedCall(cfg, "comment (stream)", func() (string, error) {
					return streamToWriter(cmd.Context(), cfg, cfg.Provider, systemPrompt, diffContent, out)
				})
				if err != nil {
					// Streaming failed; fall back to the buffered call.
					// headerPrinted=true tells the output block to skip reprinting the separator.
					comment, err = timedCall(cfg, "comment (fallback)", func() (string, error) {
						return chatFn(cmd.Context(), cfg, cfg.Provider, systemPrompt, diffContent)
					})
				} else {
					streamedOK = true
				}
			} else {
				// When a title is also needed, run comment and title concurrently to
				// save one full LLM round-trip of wall-clock time.
				needsTitle := (generateTitle || updateTitleFlag || format == "json") && !generateCommitMsg
				if needsTitle {
					debugLog(cfg, "title+comment: running in parallel")
					var parallelComment, parallelTitle string
					eg, egCtx := errgroup.WithContext(cmd.Context())
					eg.Go(func() error {
						var callErr error
						parallelComment, callErr = timedCall(cfg, "comment (parallel)", func() (string, error) {
							return chatFn(egCtx, cfg, cfg.Provider, systemPrompt, diffContent)
						})
						return callErr
					})
					eg.Go(func() error {
						var callErr error
						parallelTitle, callErr = timedCall(cfg, "title (parallel)", func() (string, error) {
							return chatFn(egCtx, cfg, cfg.Provider, titlePrompt, diffContent)
						})
						return callErr
					})
					err = eg.Wait()
					comment = parallelComment
					title = strings.TrimSpace(parallelTitle)
				} else {
					comment, err = timedCall(cfg, "comment", func() (string, error) {
						return chatFn(cmd.Context(), cfg, cfg.Provider, systemPrompt, diffContent)
					})
				}
			}
			if !generateCommitMsg && !titleOnly && err != nil {
				if cfg.Provider == Ollama && strings.Contains(err.Error(), "connection refused") {
					return fmt.Errorf("failed to connect to Ollama at %s.\nMake sure Ollama is running (try 'ollama serve') or check your configuration", cfg.OllamaEndpoint)
				}
				return err
			}

			// Generate a title when explicitly requested (--title) or when producing
			// JSON output for pipeline consumers (--format=json implies title).
			// Skip title generation entirely when --commit-msg is active.
			// NOTE: title may already be set above by the parallel path; this block
			// only runs for the streaming case where the comment was written token-by-token.
			if (generateTitle || updateTitleFlag || format == "json") && !generateCommitMsg && !titleOnly && title == "" {
				debugLog(cfg, "title: generating title after stream")
				title, err = timedCall(cfg, "title", func() (string, error) {
					return chatFn(cmd.Context(), cfg, cfg.Provider, titlePrompt, diffContent)
				})
				if err != nil {
					if cfg.Provider == Ollama && strings.Contains(err.Error(), "connection refused") {
						return fmt.Errorf("failed to connect to Ollama at %s.\nMake sure Ollama is running (try 'ollama serve') or check your configuration", cfg.OllamaEndpoint)
					}
					return err
				}
				title = strings.TrimSpace(title)
			}

			// Parse and strip the VERDICT line when --exit-code is active.
			var verdict string
			if exitCodeFlag {
				verdict, comment = parseVerdict(comment)
				if verdict != "PASS" && verdict != "FAIL" {
					verdict = "FAIL"
				}
				debugLog(cfg, "exit-code: verdict=%s", verdict)
			}

			dest := "stdout"
			if outputPath != "" {
				dest = "file: " + outputPath
			} else if clipboardFlag != "" {
				dest = "stdout+clipboard:" + clipboardFlag
			}
			debugLog(cfg, "output: format=%s destination=%s", format, dest)

			// outputJSON is the structured response emitted when --format=json is set.
			// For --commit-msg: only commit_message, provider, model are populated.
			// For normal description: title and description are the primary fields;
			// comment mirrors description for backwards compatibility.
			// Hoisted to outer scope so --output file can reference it when format=json.
			type outputJSON struct {
				Title         string `json:"title,omitempty"`
				Description   string `json:"description,omitempty"`
				Comment       string `json:"comment,omitempty"`
				CommitMessage string `json:"commit_message,omitempty"`
				Verdict       string `json:"verdict,omitempty"`
				Provider      string `json:"provider"`
				Model         string `json:"model"`
				DiffSource    string `json:"diff_source,omitempty"`
				Truncated     bool   `json:"truncated,omitempty"`
			}
			var payload outputJSON
			if titleOnly {
				payload = outputJSON{
					Title:      title,
					Provider:   string(cfg.Provider),
					Model:      getModelName(cfg),
					DiffSource: diffSource,
					Truncated:  diffTruncated,
				}
			} else if generateCommitMsg {
				payload = outputJSON{
					CommitMessage: commitMessage,
					Provider:      string(cfg.Provider),
					Model:         getModelName(cfg),
					DiffSource:    diffSource,
					Truncated:     diffTruncated,
				}
			} else {
				payload = outputJSON{
					Title:       title,
					Description: comment,
					Comment:     comment,
					Verdict:     verdict,
					Provider:    string(cfg.Provider),
					Model:       getModelName(cfg),
					DiffSource:  diffSource,
					Truncated:   diffTruncated,
				}
			}

			if verdictOnly {
				if format == "json" {
					if err := json.NewEncoder(out).Encode(struct {
						Verdict    string `json:"verdict"`
						Provider   string `json:"provider"`
						Model      string `json:"model"`
						DiffSource string `json:"diff_source,omitempty"`
						Truncated  bool   `json:"truncated,omitempty"`
					}{
						Verdict:    verdict,
						Provider:   string(cfg.Provider),
						Model:      getModelName(cfg),
						DiffSource: diffSource,
						Truncated:  diffTruncated,
					}); err != nil {
						return err
					}
				} else {
					_, _ = fmt.Fprintln(out, verdict)
				}
			} else if streamMode == "jsonl" {
				if err := encodeJSONLine(out, "start", map[string]any{
					"provider":    string(cfg.Provider),
					"model":       getModelName(cfg),
					"diff_source": diffSource,
					"truncated":   diffTruncated,
				}); err != nil {
					return err
				}
				if titleOnly {
					if err := encodeJSONLine(out, "token", map[string]any{"text": title}); err != nil {
						return err
					}
				} else if generateCommitMsg {
					if err := encodeJSONLine(out, "token", map[string]any{"text": commitMessage}); err != nil {
						return err
					}
				} else {
					if err := encodeJSONLine(out, "token", map[string]any{"text": comment}); err != nil {
						return err
					}
				}
				if err := encodeJSONLine(out, "done", map[string]any{"result": payload}); err != nil {
					return err
				}
			} else if format == "json" {
				if err := json.NewEncoder(out).Encode(payload); err != nil {
					return err
				}
			} else if titleOnly {
				_, _ = fmt.Fprintln(out, title)
			} else if generateCommitMsg {
				// --commit-msg text output: just the message, no headers, clean for shell piping.
				_, _ = fmt.Fprintln(out, commitMessage)
			} else if plain {
				if title != "" {
					_, _ = fmt.Fprintln(out, title)
					_, _ = fmt.Fprintln(out)
				}
				_, _ = fmt.Fprintln(out, comment)
			} else if streamedOK {
				// Streaming succeeded: body was already written token-by-token.
				_, _ = fmt.Fprintln(out)
				if title != "" {
					_, _ = fmt.Fprintln(out)
					_, _ = fmt.Fprintln(out, "── Title ────────────────────────────────")
					_, _ = fmt.Fprintln(out)
					_, _ = fmt.Fprintln(out, title)
					_, _ = fmt.Fprintln(out)
				}
			} else {
				if title != "" {
					_, _ = fmt.Fprintln(out)
					_, _ = fmt.Fprintln(out, "── Title ────────────────────────────────")
					_, _ = fmt.Fprintln(out)
					_, _ = fmt.Fprintln(out, title)
					_, _ = fmt.Fprintln(out)
				}
				_, _ = fmt.Fprintln(out)
				_, _ = fmt.Fprintln(out, "── Description ──────────────────────────")
				_, _ = fmt.Fprintln(out)
				_, _ = fmt.Fprintln(out, comment)
				_, _ = fmt.Fprintln(out)
			}

			if clipboardFlag != "" {
				var clipContent string
				switch clipboardFlag {
				case "title":
					clipContent = title
				case "description", "comment":
					clipContent = comment
				case "commit-msg":
					clipContent = commitMessage
				case "all":
					if title != "" {
						clipContent = title + "\n\n" + comment
					} else {
						clipContent = comment
					}
				default:
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: unknown --clipboard value %q (use title, description, commit-msg, or all)\n", clipboardFlag)
				}
				if clipContent != "" {
					if err := clipboard.WriteAll(clipContent); err != nil {
						_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not copy to clipboard: %v\n", err)
					}
				}
			}

			if outputPath != "" {
				var fileContent []byte
				if format == "json" {
					var buf bytes.Buffer
					if encErr := json.NewEncoder(&buf).Encode(payload); encErr != nil {
						return encErr
					}
					fileContent = buf.Bytes()
				} else if titleOnly {
					fileContent = []byte(title + "\n")
				} else if generateCommitMsg {
					fileContent = []byte(commitMessage + "\n")
				} else {
					fileContent = []byte(comment)
				}
				if err := os.WriteFile(outputPath, fileContent, 0600); err != nil {
					return err
				}
			}

			if updateTitleFlag || updateDescriptionFlag {
				var updateTitle *string
				var updateDescription *string
				if updateTitleFlag {
					updateTitle = &title
				}
				if updateDescriptionFlag {
					metadata, metaErr := getRemoteMetadata(cmd.Context(), cfg, prURL)
					if metaErr != nil {
						return metaErr
					}
					body := mergeManagedSection(metadata.Description, comment)
					updateDescription = &body
				}
				switch {
				case isGitHubURL(prURL):
					if err := updateGitHubPRMetadata(cmd.Context(), prURL, cfg.GitHubToken, cfg.GitHubBaseURL, updateTitle, updateDescription); err != nil {
						return err
					}
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Updated GitHub PR metadata.")
				case isGitLabURL(prURL):
					if err := updateGitLabMRMetadata(cmd.Context(), prURL, cfg.GitLabToken, cfg.GitLabBaseURL, updateTitle, updateDescription); err != nil {
						return err
					}
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Updated GitLab MR metadata.")
				default:
					return fmt.Errorf("unsupported URL %q: must be a GitHub PR (/pull/) or GitLab MR (/-/merge_requests/) URL", prURL)
				}
			}

			// --post: publish the generated comment back to the GitHub PR or GitLab MR.
			if postFlag {
				postBody := comment
				if title != "" {
					postBody = "**" + title + "**\n\n" + comment
				}
				switch {
				case isGitHubURL(prURL):
					if err := postGitHubPRComment(cmd.Context(), prURL, cfg.GitHubToken, cfg.GitHubBaseURL, postBody); err != nil {
						return err
					}
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Posted comment to GitHub PR.")
				case isGitLabURL(prURL):
					if err := postGitLabMRNote(cmd.Context(), prURL, cfg.GitLabToken, cfg.GitLabBaseURL, postBody); err != nil {
						return err
					}
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Posted note to GitLab MR.")
				}
			}

			// --exit-code: non-zero exit when AI verdict is FAIL.
			if exitCodeFlag && verdict == "FAIL" {
				return exitCodeError(2)
			}
			return nil
		},
	}

	rootCmd.Flags().StringVar(&commit, "commit", "", "Commit or commit range")
	rootCmd.Flags().StringVar(&diffFilePath, "file", "", "Path to diff file")
	rootCmd.Flags().StringVar(&prURL, "pr", "", "GitHub PR or GitLab MR URL (e.g. https://github.com/owner/repo/pull/123 or https://gitlab.com/group/project/-/merge_requests/42)")
	rootCmd.Flags().StringVar(&outputPath, "output", "", "Output file path")
	rootCmd.Flags().StringVar(&provider, "provider", "openai", "API provider (openai, anthropic, gemini, ollama)")
	rootCmd.Flags().StringVar(&modelOverride, "model", "", "Override the model for this run (e.g. gpt-5.5, claude-opus-4-6, gemini-2.5-flash)")
	rootCmd.Flags().StringVarP(&templateName, "template", "t", "default", "Prompt template to use (e.g., default, conventional, technical)")
	rootCmd.Flags().BoolVar(&debug, "debug", false, "Show token/cost estimate and exit without calling the API")
	rootCmd.Flags().BoolVar(&verbose, "verbose", false, "Enable verbose debug logging to stderr (provider, model, timing, errors)")
	rootCmd.Flags().BoolVar(&staged, "staged", false, "Diff staged changes only (git diff --cached)")
	rootCmd.Flags().StringVar(&clipboardFlag, "clipboard", "", "Copy to clipboard: title, description, or all")
	rootCmd.Flags().StringArrayVar(&exclude, "exclude", nil, "Exclude files matching pattern (e.g. vendor/**, *.sum). Can be repeated.")
	rootCmd.Flags().StringVar(&format, "format", "text", "Output format: text or json")
	rootCmd.Flags().StringVar(&inputFormat, "input", "text", "Input format: text or json")
	rootCmd.Flags().StringVar(&streamMode, "stream", "", "Structured stream mode: jsonl")
	rootCmd.Flags().StringVar(&presetName, "preset", "", "Preset defaults: ci, local-fast, security, release-notes")
	rootCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would happen without calling the provider or writing side effects")
	rootCmd.Flags().BoolVar(&changedFilesOnly, "changed-files", false, "Print changed file paths and exit without calling the provider")
	rootCmd.Flags().BoolVar(&summaryOnly, "summary-only", false, "Print diff stats and changed files and exit without calling the provider")
	rootCmd.Flags().BoolVar(&quiet, "quiet", false, "Machine mode: emit JSON on stdout and route diagnostics to stderr")
	rootCmd.Flags().BoolVar(&plain, "plain", false, "Suppress text section headers and decorations")
	rootCmd.Flags().BoolVar(&plain, "no-decorate", false, "Alias for --plain")
	rootCmd.Flags().BoolVar(&printPrompt, "print-prompt", false, "Print the resolved system prompt and exit without calling the provider")
	rootCmd.Flags().BoolVar(&printRequest, "print-request", false, "Print the resolved provider request as JSON and exit without calling the provider")
	rootCmd.Flags().BoolVar(&verdictOnly, "verdict-only", false, "Print only the parsed verdict when used with --exit-code")
	_ = rootCmd.Flags().MarkHidden("verdict-only")
	rootCmd.Flags().BoolVar(&titleOnly, "title-only", false, "Print only a generated title")
	_ = rootCmd.Flags().MarkHidden("title-only")
	rootCmd.Flags().BoolVar(&smartChunk, "smart-chunk", false, "Split large diffs by file, summarize each, then combine")
	rootCmd.Flags().BoolVar(&generateTitle, "title", false, "Generate a concise MR/PR title in addition to the comment")
	rootCmd.Flags().BoolVar(&generateCommitMsg, "commit-msg", false, "Generate a git commit message instead of a full MR/PR description")
	rootCmd.Flags().BoolVar(&multiLine, "multi-line", false, "Generate a multi-line commit message (subject + body) when used with --commit-msg; body pre-fills the PR/MR description")
	rootCmd.Flags().BoolVar(&exitCodeFlag, "exit-code", false, "Exit with code 2 if the AI detects critical issues in the diff")
	rootCmd.Flags().BoolVar(&postFlag, "post", false, "Post the generated comment back to the GitHub PR or GitLab MR (requires --pr)")
	rootCmd.Flags().BoolVar(&updateTitleFlag, "update-title", false, "Update the GitHub PR title or GitLab MR title with the generated title (requires --pr)")
	rootCmd.Flags().BoolVar(&updateDescriptionFlag, "update-description", false, "Update the GitHub PR body or GitLab MR description with the generated description (requires --pr)")
	rootCmd.Flags().StringVar(&systemPromptFlag, "system-prompt", "", `Override the system prompt for this run. Use @path to read from a file (e.g. --system-prompt=@review.txt). Mutually exclusive with --template.`)
	rootCmd.Flags().BoolVar(&estimate, "estimate", false, "Show token/cost estimate and prompt for confirmation before calling the API")
	rootCmd.Flags().BoolVarP(&autoYes, "yes", "y", false, "Auto-confirm the cost estimate prompt (use with --estimate)")
	rootCmd.Flags().BoolVar(&versionFlag, "version", false, "Print version and exit")
	rootCmd.Flags().StringVar(&profileName, "profile", "", "Named config profile to activate (defined in ~/.ai-mr-comment.toml under [profile.<name>])")
	rootCmd.Flags().BoolVar(&mrChaos, "chaos", false, "Generate a chaotic, dramatically over-the-top MR/PR description (still technically accurate)")
	rootCmd.Flags().BoolVar(&mrHaiku, "haiku", false, "Generate the entire MR/PR description as a sequence of haikus")
	rootCmd.Flags().BoolVar(&mrRoast, "roast", false, "Generate a technically accurate but sardonically judgmental MR/PR description")
	rootCmd.Flags().BoolVar(&mrIntern, "intern", false, "Generate an overly enthusiastic junior-developer MR/PR description")
	rootCmd.Flags().BoolVar(&mrShakespeare, "shakespeare", false, "Generate the MR/PR description in Shakespearean Early Modern English")
	rootCmd.Flags().BoolVar(&mrManager, "manager", false, "Generate the MR/PR description in passive-aggressive corporate non-speak")
	rootCmd.Flags().BoolVar(&mrYoda, "yoda", false, "Generate the MR/PR description in Yoda's inverted syntax")
	rootCmd.Flags().BoolVar(&mrExcuse, "excuse", false, "Generate a technically accurate MR/PR description with built-in excuses")
	_ = rootCmd.RegisterFlagCompletionFunc("provider", completeValues(providerNames))
	_ = rootCmd.RegisterFlagCompletionFunc("template", completeValues(templateNames))
	_ = rootCmd.RegisterFlagCompletionFunc("preset", completeValues(presetNames))
	_ = rootCmd.RegisterFlagCompletionFunc("profile", completeProfiles)
	_ = rootCmd.RegisterFlagCompletionFunc("format", completeValues([]string{"text", "json"}))
	_ = rootCmd.RegisterFlagCompletionFunc("input", completeValues([]string{"text", "json"}))
	_ = rootCmd.RegisterFlagCompletionFunc("stream", completeValues([]string{"jsonl"}))
	_ = rootCmd.RegisterFlagCompletionFunc("clipboard", completeValues([]string{"title", "description", "comment", "commit-msg", "all"}))

	rootCmd.AddCommand(newInitConfigCmd())
	rootCmd.AddCommand(newModelsCmd())
	rootCmd.AddCommand(newCheckCmd(chatFn))
	rootCmd.AddCommand(newDoctorCmd())
	rootCmd.AddCommand(newQuickCommitCmd(chatFn))
	rootCmd.AddCommand(newPublishCmd(chatFn))
	rootCmd.AddCommand(newChangelogCmd(chatFn))
	rootCmd.AddCommand(newAgentAliasCmd("review", "Generate a review from a diff", nil, chatFn))
	rootCmd.AddCommand(newAgentAliasCmd("title", "Generate only a PR/MR title", []string{"--title-only", "--plain"}, chatFn))
	rootCmd.AddCommand(newAgentAliasCmd("commit-message", "Generate only a commit message", []string{"--commit-msg"}, chatFn))
	rootCmd.AddCommand(newAgentAliasCmd("verdict", "Generate only a PASS/FAIL verdict", []string{"--exit-code", "--verdict-only", "--plain"}, chatFn))
	rootCmd.AddCommand(newAgentAliasCmd("estimate", "Estimate prompt tokens and input cost", []string{"--debug"}, chatFn))
	rootCmd.AddCommand(newGenAliasesCmd())
	rootCmd.AddCommand(newGenWorkflowCmd())

	rootCmd.AddCommand(&cobra.Command{
		Use:       "completion [bash|zsh|fish|powershell]",
		Short:     "Generate shell completion script",
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			switch args[0] {
			case "bash":
				return rootCmd.GenBashCompletionV2(out, true)
			case "zsh":
				return rootCmd.GenZshCompletion(out)
			case "fish":
				return rootCmd.GenFishCompletion(out, true)
			case "powershell":
				return rootCmd.GenPowerShellCompletionWithDesc(out)
			default:
				return fmt.Errorf("unsupported shell: %s", args[0])
			}
		},
	})

	return rootCmd
}

func newAgentAliasCmd(name, short string, prefix []string, chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error)) *cobra.Command {
	return &cobra.Command{
		Use:                name,
		Short:              short,
		DisableFlagParsing: true,
		SilenceErrors:      true,
		SilenceUsage:       true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := newRootCmd(chatFn)
			root.SetIn(cmd.InOrStdin())
			root.SetOut(cmd.OutOrStdout())
			root.SetErr(cmd.ErrOrStderr())
			root.SilenceErrors = true
			root.SilenceUsage = true
			translated := make([]string, 0, len(prefix)+len(args))
			translated = append(translated, prefix...)
			translated = append(translated, args...)
			root.SetArgs(translated)
			return root.Execute()
		},
	}
}

// defaultConfigTOML is the template written by the init-config subcommand.
// It documents every supported key with its default value.
const defaultConfigTOML = `# ai-mr-comment configuration
# Place this file at ~/.ai-mr-comment.toml or in the project root.

# Default AI provider: openai | anthropic | gemini | ollama | claude-cli | gemini-cli | codex-cli
provider = "anthropic"

# Default prompt template.
# Built-in options: default | conventional | technical | user-focused | emoji | sassy | monday
# You can also create custom templates in ~/.config/ai-mr-comment/templates/<name>.tmpl
template = "default"

# Optional timeout for network/CLI provider calls. Use Go duration syntax, e.g. "2m" or "30s".
# "0s" disables the timeout and preserves the provider/client default behavior.
request_timeout = "0s"

# --- OpenAI ---
# openai_api_key = ""   # or set OPENAI_API_KEY env var
openai_model    = "gpt-5.5"
openai_endpoint = "https://api.openai.com/v1/"
# Other OpenAI models: gpt-5.4, gpt-5.4-mini, gpt-5.4-nano, gpt-4.1, gpt-4.1-mini

# --- Anthropic ---
# anthropic_api_key = ""   # or set ANTHROPIC_API_KEY env var
anthropic_model    = "claude-sonnet-4-6"
anthropic_endpoint = "https://api.anthropic.com/"
# Other Anthropic models: claude-opus-4-6, claude-haiku-4-5-20251001

# --- Google Gemini ---
# gemini_api_key = ""   # or set GEMINI_API_KEY env var
gemini_model = "gemini-2.5-flash"
# Other Gemini models: gemini-2.5-pro, gemini-2.5-flash-lite, gemini-3-flash-preview, gemini-3-pro-preview

# --- Ollama (local) ---
ollama_model    = "llama3.2"
ollama_endpoint = "http://localhost:11434/api/generate"
# Other Ollama models: llama3.1, llama3, mistral, codellama, phi3

# --- Claude CLI (claude-cli) ---
# Uses the local claude CLI for auth — no API key required.
# Auth is delegated to the claude CLI process (e.g. Claude Code session).
# claude_cli_path = ""              # auto-detected: ~/.claude/local/claude, then PATH
claude_cli_model = "claude-sonnet-4-6"

# --- Gemini CLI (gemini-cli) ---
# Uses the local gemini CLI with Google OAuth — no API key required.
# Install: npm install -g @google/gemini-cli
# gemini_cli_path = ""              # auto-detected via PATH
gemini_cli_model = "gemini-2.5-flash"

# --- Codex CLI (codex-cli) ---
# Uses the local OpenAI Codex CLI in quiet mode — requires OPENAI_API_KEY.
# Install: npm install -g @openai/codex
# codex_cli_path = ""               # auto-detected via PATH
# codex_cli_model = ""              # leave empty to use codex default

# --- GitHub / GitHub Enterprise ---
# github_token = ""    # or set GITHUB_TOKEN env var
# github_base_url = "" # GitHub Enterprise host, e.g. https://github.mycompany.com

# --- GitLab / Self-Hosted GitLab ---
# gitlab_token = ""    # or set GITLAB_TOKEN env var
# gitlab_base_url = "" # Self-hosted GitLab host, e.g. https://gitlab.mycompany.com

# ---------------------------------------------------------------------------
# Named Profiles
# Switch profiles with: ai-mr-comment --profile <name>
# A profile overrides any top-level setting for that invocation only.
# ---------------------------------------------------------------------------

# Fast / cheap — gpt-5.4-nano for quick reviews and commit messages
[profile.fast]
provider     = "openai"
openai_model = "gpt-5.4-nano"
template     = "conventional"

# OpenAI — gpt-5.5 with technical template for thorough reviews
[profile.openai]
provider     = "openai"
openai_model = "gpt-5.5"
template     = "technical"

# Anthropic — claude-opus-4-6 with technical template
[profile.anthropic]
provider        = "anthropic"
anthropic_model = "claude-opus-4-6"
template        = "technical"

# Gemini — gemini-3-pro-preview with technical template
[profile.gemini]
provider     = "gemini"
gemini_model = "gemini-3-pro-preview"
template     = "technical"

# Local / offline — Ollama, no API key required
[profile.local]
provider     = "ollama"
ollama_model = "llama3.2"
template     = "default"

# Claude CLI — delegates auth to the local claude CLI (Claude Code session)
[profile.claude-cli]
provider         = "claude-cli"
claude_cli_model = "claude-sonnet-4-6"
template         = "default"

# Gemini CLI — delegates auth to the local gemini CLI (Google OAuth)
[profile.gemini-cli]
provider         = "gemini-cli"
gemini_cli_model = "gemini-2.5-flash"
template         = "default"

# Codex CLI — uses the local codex CLI (requires OPENAI_API_KEY)
[profile.codex-cli]
provider = "codex-cli"
template = "default"
`

// newInitConfigCmd returns the init-config subcommand, which writes a commented
// TOML configuration file to the destination path (default: ~/.ai-mr-comment.toml).
func newInitConfigCmd() *cobra.Command {
	var outputPath string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "init-config",
		Short: "Write a default config file to ~/.ai-mr-comment.toml",
		Long: `Writes a commented TOML configuration file with all supported settings and
their defaults. Edit the generated file to add your API keys and customise
models, endpoints, or the default provider.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dest := outputPath
			if dest == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("could not determine home directory: %w", err)
				}
				dest = home + "/.ai-mr-comment.toml"
			}

			if dryRun {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Dry run: would write config file to %s\n", dest)
				return nil
			}

			if _, err := os.Stat(dest); err == nil {
				return fmt.Errorf("config file already exists at %s (remove it first or use --output to choose a different path)", dest)
			}

			if err := os.WriteFile(dest, []byte(defaultConfigTOML), 0600); err != nil {
				return fmt.Errorf("could not write config file: %w", err)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Config file written to %s\n", dest)
			return nil
		},
	}

	cmd.Flags().StringVar(&outputPath, "output", "", "Write config to this path instead of ~/.ai-mr-comment.toml")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print destination without writing the config file")
	return cmd
}

// getModelName returns the configured model name for the active provider.
func getModelName(cfg *Config) string {
	switch cfg.Provider {
	case OpenAI:
		return cfg.OpenAIModel
	case Anthropic:
		return cfg.AnthropicModel
	case Gemini:
		return cfg.GeminiModel
	case Ollama:
		return cfg.OllamaModel
	case ClaudeCLI:
		return cfg.ClaudeCLIModel
	case GeminiCLI:
		return cfg.GeminiCLIModel
	case CodexCLI:
		return cfg.CodexCLIModel
	default:
		return "unknown"
	}
}

func validateProviderConfig(cfg *Config) error {
	if !isSupportedProvider(cfg.Provider) {
		return errors.New("unsupported provider: " + string(cfg.Provider))
	}
	// Delegate API key validation to validateAPIKey (same check used by chatCompletions).
	return validateAPIKey(cfg.Provider, cfg)
}

func applyRequestTimeout(cmd *cobra.Command, cfg *Config) context.CancelFunc {
	if cfg.RequestTimeout <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), cfg.RequestTimeout)
	cmd.SetContext(ctx)
	return cancel
}

// debugLog writes a formatted debug message to cfg.DebugWriter when verbose mode is enabled.
// The message is prefixed with "[debug] " and terminated with a newline.
func debugLog(cfg *Config, format string, args ...any) {
	if cfg.DebugWriter == nil {
		return
	}
	debugWriterMu.Lock()
	defer debugWriterMu.Unlock()
	_, _ = fmt.Fprintf(cfg.DebugWriter, "[debug] "+format+"\n", args...)
}

// timedCall invokes fn, then logs the elapsed time, response size, and any error.
// It is a no-op when verbose mode is disabled (cfg.DebugWriter == nil).
func timedCall(cfg *Config, label string, fn func() (string, error)) (string, error) {
	start := time.Now()
	result, err := fn()
	elapsed := time.Since(start).Milliseconds()
	if err == nil {
		debugLog(cfg, "api: %s completed in %dms chars=%d lines=%d",
			label, elapsed, len(result), len(strings.Split(result, "\n")))
	} else {
		debugLog(cfg, "api: %s failed in %dms: %v", label, elapsed, err)
	}
	return result, err
}

// showCostEstimate prints token and cost estimation to w.
func showCostEstimate(ctx context.Context, cfg *Config, systemPrompt, diffContent string, w io.Writer) {
	model := getModelName(cfg)
	estimator := NewTokenEstimator(cfg)
	totalTokens, err := estimator.CountTokens(ctx, model, systemPrompt, diffContent)
	if err != nil {
		_, _ = fmt.Fprintf(w, "Error estimating tokens: %v\n", err)
		fallback := &HeuristicTokenEstimator{}
		totalTokens, _ = fallback.CountTokens(context.Background(), "", systemPrompt, diffContent)
		_, _ = fmt.Fprintln(w, "Using heuristic fallback.")
	}
	cost := EstimateCost(model, totalTokens)
	_, _ = fmt.Fprintln(w, "Token & Cost Estimation:")
	_, _ = fmt.Fprintf(w, "- Model: %s\n", model)
	_, _ = fmt.Fprintf(w, "- Diff lines: %d\n", strings.Count(diffContent, "\n")+1)
	_, _ = fmt.Fprintf(w, "- Estimated Input Tokens: %d\n", totalTokens)
	_, _ = fmt.Fprintf(w, "- Estimated Input Cost: $%.6f\n", cost)
	_, _ = fmt.Fprintln(w, "\nNote: Output tokens and cost depend on the generated response length.")
}

// promptConfirm writes a "Proceed? [y/N]: " prompt to promptWriter and reads
// one line from stdinReader. Returns true only if the user types "y" or "Y".
// Auto-confirms when autoYes is true. Auto-declines when stdinReader is not
// an interactive terminal (e.g. in CI or piped input).
func promptConfirm(promptWriter io.Writer, stdinReader io.Reader, autoYes bool) bool {
	if autoYes {
		return true
	}
	if f, ok := stdinReader.(*os.File); ok {
		if !fileIsTerminal(f) {
			_, _ = fmt.Fprintln(promptWriter, "Non-interactive mode: auto-declining. Use --yes to proceed.")
			return false
		}
	} else {
		// Non-*os.File reader (e.g. strings.Reader in tests) is non-interactive.
		_, _ = fmt.Fprintln(promptWriter, "Non-interactive mode: auto-declining. Use --yes to proceed.")
		return false
	}
	_, _ = fmt.Fprint(promptWriter, "Proceed? [y/N]: ")
	var line string
	_, _ = fmt.Fscan(stdinReader, &line)
	return strings.ToLower(strings.TrimSpace(line)) == "y"
}

func fileIsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd())) //nolint:gosec // G115: os.File descriptors are small OS-provided handles expected by x/term.
}

// setModelOverride applies a CLI --model value to the correct provider field in cfg.
func setModelOverride(cfg *Config, model string) {
	switch cfg.Provider {
	case OpenAI:
		cfg.OpenAIModel = model
	case Anthropic:
		cfg.AnthropicModel = model
	case Gemini:
		cfg.GeminiModel = model
	case Ollama:
		cfg.OllamaModel = model
	case ClaudeCLI:
		cfg.ClaudeCLIModel = model
	case GeminiCLI:
		cfg.GeminiCLIModel = model
	case CodexCLI:
		cfg.CodexCLIModel = model
	}
}

// providerModels lists known models for each provider, used by the `models` subcommand.
var providerModels = map[ApiProvider][]string{
	OpenAI: {
		"gpt-5.5",
		"gpt-5.4",
		"gpt-5.4-mini",
		"gpt-5.4-nano",
		"gpt-4.1",
		"gpt-4.1-mini",
		"gpt-4.1-nano",
		"o3",
		"o3-mini",
		"gpt-4o",
		"gpt-4o-mini",
	},
	Anthropic: {
		"claude-opus-4-6",
		"claude-sonnet-4-6",
		"claude-haiku-4-5-20251001",
		"claude-opus-4-5-20251101",
		"claude-sonnet-4-5-20250929",
		"claude-sonnet-4-20250514",
		"claude-3-7-sonnet-20250219",
		"claude-3-haiku-20240307",
	},
	Gemini: {
		"gemini-2.5-pro",
		"gemini-2.5-flash",
		"gemini-2.5-flash-lite",
		"gemini-3-flash-preview",
		"gemini-3-pro-preview",
		"gemini-2.0-flash",
	},
	Ollama: {
		"llama3",
		"llama3.1",
		"llama3.2",
		"mistral",
		"codellama",
		"phi3",
	},
	ClaudeCLI: {
		"claude-opus-4-6",
		"claude-sonnet-4-6",
		"claude-haiku-4-5-20251001",
	},
	GeminiCLI: {
		"gemini-2.5-pro",
		"gemini-2.5-flash",
		"gemini-2.5-flash-lite",
	},
	CodexCLI: {
		// Model names depend on what is configured in the local codex CLI.
		// Leave empty to use codex default, or set codex_cli_model in config.
	},
}

// newModelsCmd returns the models subcommand, which lists known models for the active provider.
func newModelsCmd() *cobra.Command {
	var provider string

	cmd := &cobra.Command{
		Use:   "models",
		Short: "List available models for a provider",
		Long:  `Prints the known model names for the given provider. Use --provider to select a provider (defaults to the configured one).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := ApiProvider(provider)
			models, ok := providerModels[p]
			if !ok {
				return fmt.Errorf("unknown provider %q: choose from openai, anthropic, gemini, ollama, claude-cli, gemini-cli, codex-cli", provider)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Models for provider %s:\n\n", p)
			if len(models) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  (no fixed model list — configured by the local CLI tool)")
			}
			for _, m := range models {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", m)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Use --model <name> to select a model for a run.\n")
			return nil
		},
	}

	cmd.Flags().StringVar(&provider, "provider", "anthropic", "Provider to list models for (openai, anthropic, gemini, ollama, claude-cli, gemini-cli, codex-cli)")
	_ = cmd.RegisterFlagCompletionFunc("provider", completeValues(providerNames))
	return cmd
}

// newQuickCommitCmd returns a command that stages all changes, generates an
// AI commit message, commits, and pushes — all in one step.
func newQuickCommitCmd(chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error)) *cobra.Command {
	var provider, modelOverride, format, profileName, commitType, commitScope, messageTemplate string
	var dryRun, noPush, breaking, multiLine, longBody, emoji, noConventional, postFlag, verbose bool
	var editMessage, includeUntracked, trackedOnly, signoff bool
	var chaos, haiku, roast, fortune bool
	var qcMonday, qcJira, qcEmoji, qcSassy, qcTechnical bool
	var qcIntern, qcShakespeare, qcManager, qcYoda, qcExcuse bool
	var bodyLines int

	cmd := &cobra.Command{
		Use:   "quick-commit",
		Short: "Stage, AI-commit, and push in one step",
		Long: `Stages all changes (git add .), generates a conventional commit message
using AI, commits with that message, and pushes to the current branch's
remote. Use --dry-run to preview the generated message without committing.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isGitRepo() {
				return fmt.Errorf("not a git repository")
			}

			cfg, err := loadConfigForProfile(profileName)
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("provider") {
				cfg.Provider = ApiProvider(provider)
			}
			if cmd.Flags().Changed("model") {
				setModelOverride(cfg, modelOverride)
			}
			if verbose {
				cfg.DebugWriter = cmd.ErrOrStderr()
			}
			if cfgErr := validateProviderConfig(cfg); cfgErr != nil {
				return cfgErr
			}
			if cancel := applyRequestTimeout(cmd, cfg); cancel != nil {
				defer cancel()
			}
			if format != "text" && format != "json" {
				return fmt.Errorf("unsupported format %q: must be text or json", format)
			}
			if bodyLines < 0 {
				return fmt.Errorf("--body-lines must be 0 or greater")
			}
			commitType = strings.ToLower(strings.TrimSpace(commitType))
			commitScope = strings.TrimSpace(commitScope)
			messageTemplate = strings.ToLower(strings.TrimSpace(messageTemplate))
			if commitType != "" && !isValidCommitType(commitType) {
				return fmt.Errorf("--type must be one of: %s", strings.Join(conventionalCommitTypes, ", "))
			}
			if messageTemplate != "" && !isValidQuickCommitMessageTemplate(messageTemplate) {
				return fmt.Errorf("--message-template must be one of: %s", strings.Join(quickCommitMessageTemplateNames, ", "))
			}
			if cmd.Flags().Changed("scope") {
				if err := validateCommitScope(commitScope); err != nil {
					return err
				}
			}
			if noConventional && (commitType != "" || commitScope != "") {
				return fmt.Errorf("--type and --scope cannot be combined with --no-conventional")
			}
			if breaking && commitType != "" && commitType != "feat" {
				return fmt.Errorf("--breaking can only be combined with --type=feat")
			}
			if includeUntracked && trackedOnly {
				return fmt.Errorf("--include-untracked and --tracked-only are mutually exclusive")
			}
			if longBody || bodyLines > 0 || quickCommitMessageTemplateImpliesBody(messageTemplate) {
				multiLine = true
			}
			if postFlag && dryRun {
				return errors.New("--post cannot be combined with --dry-run")
			}
			if postFlag && noPush {
				return errors.New("--post cannot be combined with --no-push")
			}

			branch, err := getCurrentBranch()
			if err != nil {
				return fmt.Errorf("could not determine current branch: %w", err)
			}
			if branch == "" {
				return fmt.Errorf("cannot quick-commit in detached HEAD state")
			}

			out := cmd.OutOrStdout()

			// Stage everything (skipped in dry-run).
			if !dryRun {
				if trackedOnly {
					_, _ = fmt.Fprintln(out, "Staging tracked changes (git add -u)...")
				} else {
					_, _ = fmt.Fprintln(out, "Staging all changes (git add .)...")
				}
				if err := stageQuickCommitChanges(trackedOnly); err != nil {
					return err
				}
			}

			// Get the diff to feed to the AI.
			// After a real git add the staged diff is the right source.
			// In dry-run mode nothing was staged, so use the full working-tree diff
			// (staged + unstaged) so the preview is still meaningful.
			var diffContent string
			if dryRun {
				diffContent, err = getGitDiff("", false, nil)
			} else {
				diffContent, err = getGitDiff("", true, nil)
			}
			if err != nil {
				return fmt.Errorf("reading diff: %w", err)
			}
			if strings.TrimSpace(diffContent) == "" && !chaos {
				return fmt.Errorf("no changes found to generate a commit message for")
			}

			// Validate mutually exclusive style flags.
			styleFlagNames := []string{"--chaos", "--haiku", "--roast", "--monday", "--jira", "--emoji-commit", "--sassy", "--technical", "--intern", "--shakespeare", "--manager", "--yoda", "--excuse"}
			styleFlags := []bool{chaos, haiku, roast, qcMonday, qcJira, qcEmoji, qcSassy, qcTechnical, qcIntern, qcShakespeare, qcManager, qcYoda, qcExcuse}
			styleCount := 0
			for _, f := range styleFlags {
				if f {
					styleCount++
				}
			}
			if styleCount > 1 {
				return fmt.Errorf("%s are mutually exclusive", strings.Join(styleFlagNames, ", "))
			}
			if chaos && (multiLine || noConventional) {
				return fmt.Errorf("--chaos cannot be combined with --multi-line or --no-conventional")
			}
			if haiku && (multiLine || noConventional) {
				return fmt.Errorf("--haiku cannot be combined with --multi-line or --no-conventional")
			}
			if roast && (multiLine || noConventional) {
				return fmt.Errorf("--roast cannot be combined with --multi-line or --no-conventional")
			}

			// Prepend branch name so the AI can reference the ticket key.
			diffContent = "Branch: " + branch + "\n\n" + diffContent
			diffContent = processDiff(diffContent, 4000)

			// --chaos ignores the real diff; just pass a fixed token.
			if chaos {
				diffContent = "chaos mode"
			}

			// Generate commit message via AI.
			var prompt string
			switch {
			case chaos:
				prompt = quickCommitChaosPrompt
			case haiku:
				prompt = quickCommitHaikuPrompt
			case roast:
				prompt = quickCommitRoastPrompt
			case qcMonday:
				prompt = quickCommitMondayPrompt
			case qcJira:
				prompt = quickCommitJiraPrompt
			case qcEmoji:
				prompt = quickCommitEmojiPrompt
			case qcSassy:
				prompt = quickCommitSassyPrompt
			case qcTechnical:
				prompt = quickCommitTechnicalPrompt
			case qcIntern:
				prompt = quickCommitInternPrompt
			case qcShakespeare:
				prompt = quickCommitShakespearePrompt
			case qcManager:
				prompt = quickCommitManagerPrompt
			case qcYoda:
				prompt = quickCommitYodaPrompt
			case qcExcuse:
				prompt = quickCommitExcusePrompt
			case multiLine:
				prompt = commitMsgBodyPrompt
				if longBody || bodyLines > 0 {
					prompt += longCommitBodyPromptSuffix(bodyLines)
				}
			case noConventional:
				prompt = quickCommitFreePrompt
			default:
				prompt = quickCommitPrompt
			}
			if breaking {
				prompt += "\n\nThis is a BREAKING CHANGE release. You MUST use the 'feat!' type (with an exclamation mark) to signal a breaking change, e.g. \"feat!(scope): description\" or \"feat!: description\"."
				diffContent += "\n\nBREAKING CHANGE: this release introduces a breaking change and must use the feat! conventional commit type."
			}
			prompt = appendCommitGuidance(prompt, commitType, commitScope, messageTemplate)
			commitMessage, err := chatFn(cmd.Context(), cfg, cfg.Provider, prompt, diffContent)
			if err != nil {
				return fmt.Errorf("generating commit message: %w", err)
			}
			if multiLine {
				commitMessage = normalizeCommitBody(commitMessage)
			} else {
				commitMessage = normalizeCommitMessage(commitMessage)
			}
			if breaking {
				commitMessage = enforceBreakingChange(commitMessage)
				// Append a BREAKING CHANGE footer so semantic-release detects the
				// major bump even when the commit is squashed into a merge commit
				// (where the subject line is replaced by "Merge pull request #N...").
				commitMessage += "\n\nBREAKING CHANGE: breaking change"
			}
			commitMessage = applyCommitTypeScope(commitMessage, commitType, commitScope)
			if breaking {
				commitMessage = enforceBreakingChange(commitMessage)
			}
			if emoji {
				commitMessage = appendCommitEmoji(commitMessage)
			}
			if commitMessage == "" {
				return fmt.Errorf("AI returned an empty commit message")
			}

			// Generate a fortune trailer if requested.
			var fortuneBody string
			if fortune {
				rawFortune, fortuneErr := chatFn(cmd.Context(), cfg, cfg.Provider, fortunePrompt, "generate a fortune")
				if fortuneErr != nil {
					return fmt.Errorf("generating fortune: %w", fortuneErr)
				}
				fortuneBody = strings.TrimSpace(rawFortune)
			}

			jsonMsg := commitMessage
			if fortuneBody != "" {
				jsonMsg += "\n\n" + fortuneBody
			}
			if editMessage {
				edited, editErr := editCommitMessage(jsonMsg)
				if editErr != nil {
					return editErr
				}
				jsonMsg = edited
				commitMessage = edited
				fortuneBody = ""
			}
			if signoff {
				identity, signoffErr := getGitSignoffIdentity()
				if signoffErr != nil {
					return fmt.Errorf("--signoff: %w", signoffErr)
				}
				jsonMsg = appendSignedOffBy(jsonMsg, identity)
				commitMessage = jsonMsg
				fortuneBody = ""
			}
			if format == "json" {
				if err := json.NewEncoder(out).Encode(struct {
					CommitMessage string `json:"commit_message"`
					Provider      string `json:"provider"`
					Model         string `json:"model"`
				}{
					CommitMessage: jsonMsg,
					Provider:      string(cfg.Provider),
					Model:         getModelName(cfg),
				}); err != nil {
					return err
				}
			} else {
				_, _ = fmt.Fprintf(out, "%s\n\n", commitMessage)
				if fortuneBody != "" {
					_, _ = fmt.Fprintf(out, "%s\n\n", fortuneBody)
				}
			}

			if dryRun {
				if format != "json" {
					_, _ = fmt.Fprintln(out, "(dry-run: no changes committed)")
				}
				return nil
			}

			// Commit.
			if format != "json" {
				_, _ = fmt.Fprintln(out, "Committing...")
			}
			if err := gitCommitMessage(jsonMsg); err != nil {
				return err
			}

			if noPush {
				if format != "json" {
					_, _ = fmt.Fprintln(out, "Done. (skipped push)")
				}
				return nil
			}

			// Push.
			if format != "json" {
				_, _ = fmt.Fprintf(out, "Pushing to origin/%s...\n", branch)
			}
			if err := gitPush(branch); err != nil {
				return err
			}

			if format != "json" {
				_, _ = fmt.Fprintln(out, "Done.")
				if remoteURL, remErr := getRemoteURL(); remErr == nil {
					if createURL := prCreateURL(remoteURL, branch); createURL != "" {
						_, _ = fmt.Fprintf(out, "\nOpen PR/MR: %s\n", createURL)
					}
				}
			}

			if postFlag {
				remoteURL, remErr := getRemoteURL()
				if remErr != nil {
					return fmt.Errorf("--post: getting remote URL: %w", remErr)
				}
				info, parseErr := parseRemoteInfo(remoteURL)
				if parseErr != nil {
					return fmt.Errorf("--post: %w", parseErr)
				}

				// Derive PR/MR title from the commit message subject line.
				subject, _, _ := strings.Cut(commitMessage, "\n")

				// Find an existing open PR/MR or create a new one.
				var prMRURL string
				switch {
				case isGitHubHost(info.Host, cfg.GitHubBaseURL):
					if len(info.PathParts) < 2 {
						return fmt.Errorf("--post: could not parse owner/repo from remote URL")
					}
					prMRURL, err = findOrCreateGitHubPRFromConfig(cmd.Context(), cfg, info.PathParts[0], info.PathParts[1], branch, subject)
				case isGitLabHost(info.Host, cfg.GitLabBaseURL):
					prMRURL, err = findOrCreateGitLabMRFromConfig(cmd.Context(), cfg, strings.Join(info.PathParts, "/"), branch, subject)
				default:
					return fmt.Errorf("--post: unrecognised remote host %q; set github_base_url or gitlab_base_url in config", info.Host)
				}
				if err != nil {
					return fmt.Errorf("--post: finding or creating PR/MR: %w", err)
				}
				_, _ = fmt.Fprintf(out, "PR/MR: %s\n", prMRURL)

				// Fetch the PR/MR diff from the remote API.
				_, _ = fmt.Fprintln(out, "Generating AI review comment...")
				var diffForReview string
				switch {
				case isGitHubHost(info.Host, cfg.GitHubBaseURL):
					diffForReview, err = getPRDiff(cmd.Context(), prMRURL, cfg.GitHubToken, cfg.GitHubBaseURL)
				case isGitLabHost(info.Host, cfg.GitLabBaseURL):
					diffForReview, err = getMRDiff(cmd.Context(), prMRURL, cfg.GitLabToken, cfg.GitLabBaseURL)
				}
				if err != nil {
					return fmt.Errorf("--post: fetching PR/MR diff: %w", err)
				}

				// Generate the AI review using the default MR system prompt.
				reviewComment, reviewErr := chatFn(cmd.Context(), cfg, cfg.Provider, defaultPromptTemplate, diffForReview)
				if reviewErr != nil {
					return fmt.Errorf("--post: generating review comment: %w", reviewErr)
				}

				// Post the comment back to the PR/MR.
				switch {
				case isGitHubHost(info.Host, cfg.GitHubBaseURL):
					err = postGitHubPRComment(cmd.Context(), prMRURL, cfg.GitHubToken, cfg.GitHubBaseURL, reviewComment)
					if err == nil {
						_, _ = fmt.Fprintln(out, "Posted AI review comment to GitHub PR.")
					}
				case isGitLabHost(info.Host, cfg.GitLabBaseURL):
					err = postGitLabMRNote(cmd.Context(), prMRURL, cfg.GitLabToken, cfg.GitLabBaseURL, reviewComment)
					if err == nil {
						_, _ = fmt.Fprintln(out, "Posted AI review note to GitLab MR.")
					}
				}
				if err != nil {
					return fmt.Errorf("--post: posting comment: %w", err)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&provider, "provider", "openai", "AI provider to use (openai, anthropic, gemini, ollama)")
	cmd.Flags().StringVar(&modelOverride, "model", "", "Override the model for this run")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text or json")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Generate and print the commit message without staging, committing, or pushing")
	cmd.Flags().BoolVar(&noPush, "no-push", false, "Commit but skip the push step")
	cmd.Flags().BoolVar(&editMessage, "edit", false, "Open the generated commit message in $GIT_EDITOR, $VISUAL, or $EDITOR before committing")
	cmd.Flags().BoolVar(&includeUntracked, "include-untracked", false, "Explicitly stage tracked and untracked changes (default behaviour)")
	cmd.Flags().BoolVar(&trackedOnly, "tracked-only", false, "Stage only tracked modified/deleted files with git add -u")
	cmd.Flags().BoolVar(&signoff, "signoff", false, "Append a Signed-off-by trailer using git user.name and user.email")
	cmd.Flags().StringVar(&commitType, "type", "", "Force a conventional commit type (feat, fix, docs, style, refactor, test, chore, perf, ci, build, revert)")
	cmd.Flags().StringVar(&commitScope, "scope", "", "Force a conventional commit scope")
	cmd.Flags().StringVar(&messageTemplate, "message-template", "", "Apply a commit message template style (short, detailed, release, ticket)")
	cmd.Flags().BoolVar(&postFlag, "post", false, "After pushing, find or create a PR/MR and post an AI review comment (requires GITHUB_TOKEN or GITLAB_TOKEN)")
	cmd.Flags().BoolVar(&breaking, "breaking", false, "Mark as a breaking change: forces feat! conventional commit type for a major version bump")
	cmd.Flags().BoolVar(&multiLine, "multi-line", false, "Generate a multi-line commit message (subject + body) that pre-fills the PR/MR title and description")
	cmd.Flags().BoolVar(&longBody, "long", false, "Generate a longer multi-section commit body (implies --multi-line)")
	cmd.Flags().IntVar(&bodyLines, "body-lines", 0, "Target body line count for long multi-line commits (implies --multi-line)")
	cmd.Flags().BoolVar(&emoji, "emoji", false, "Append a type-matched gitmoji to the commit subject (e.g. feat → ✨, fix → 🐛, breaking → 💥)")
	cmd.Flags().BoolVar(&noConventional, "no-conventional", false, "Disable conventional commits enforcement (use the AI output as-is)")
	cmd.Flags().BoolVar(&chaos, "chaos", false, "Generate a random funny/absurd conventional commit message (great for pipeline trigger commits)")
	cmd.Flags().BoolVar(&haiku, "haiku", false, "Generate the commit message description as a 5-7-5 haiku about the diff")
	cmd.Flags().BoolVar(&roast, "roast", false, "Generate a technically accurate but passive-aggressively judgmental commit message")
	cmd.Flags().BoolVar(&fortune, "fortune", false, "Append a developer-wisdom fortune-cookie quote as a commit message trailer")
	cmd.Flags().BoolVar(&qcMonday, "monday", false, "Generate a casual, low-energy Monday-morning style commit message")
	cmd.Flags().BoolVar(&qcJira, "jira", false, "Prefix commit message with Jira ticket key extracted from the branch name")
	cmd.Flags().BoolVar(&qcEmoji, "emoji-commit", false, "Append a type-matched gitmoji to the commit description")
	cmd.Flags().BoolVar(&qcSassy, "sassy", false, "Generate a sassy but technically accurate commit message")
	cmd.Flags().BoolVar(&qcTechnical, "technical", false, "Generate a commit message with maximum technical precision")
	cmd.Flags().BoolVar(&qcIntern, "intern", false, "Generate an overly enthusiastic junior-developer commit message")
	cmd.Flags().BoolVar(&qcShakespeare, "shakespeare", false, "Generate the commit description in Shakespearean Early Modern English")
	cmd.Flags().BoolVar(&qcManager, "manager", false, "Generate the commit description in passive-aggressive corporate non-speak")
	cmd.Flags().BoolVar(&qcYoda, "yoda", false, "Generate the commit description in Yoda's inverted syntax")
	cmd.Flags().BoolVar(&qcExcuse, "excuse", false, "Generate a technically accurate commit message with a built-in excuse")
	cmd.Flags().StringVar(&profileName, "profile", "", "Named config profile to activate (defined in ~/.ai-mr-comment.toml under [profile.<name>])")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Print debug info (provider, model, prompt size, timing) to stderr")
	_ = cmd.RegisterFlagCompletionFunc("provider", completeValues(providerNames))
	_ = cmd.RegisterFlagCompletionFunc("format", completeValues([]string{"text", "json"}))
	_ = cmd.RegisterFlagCompletionFunc("type", completeValues(conventionalCommitTypes))
	_ = cmd.RegisterFlagCompletionFunc("message-template", completeValues(quickCommitMessageTemplateNames))
	_ = cmd.RegisterFlagCompletionFunc("profile", completeProfiles)
	return cmd
}

const managedDescriptionStart = "<!-- ai-mr-comment:description:start -->"
const managedDescriptionEnd = "<!-- ai-mr-comment:description:end -->"
const managedCommentMarker = "<!-- ai-mr-comment:comment -->"

func newPublishCmd(chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error)) *cobra.Command {
	var prURL, provider, modelOverride, templateName, profileName, format string
	var dryRun, noUpdateTitle, noUpdateDescription, replaceDescription, postSummary, autoLabels, draftIfRisky bool
	var labels, reviewers []string

	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Generate and sync PR/MR title, description, labels, reviewers, and managed summary",
		Long: `Generates a title and description, then synchronizes them to a remote
GitHub PR or GitLab MR. Pass --pr to target an existing PR/MR, or omit --pr to
find or create one from the current branch and origin remote.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigForProfile(profileName)
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("provider") {
				cfg.Provider = ApiProvider(provider)
			}
			if cmd.Flags().Changed("model") {
				setModelOverride(cfg, modelOverride)
			}
			if cmd.Flags().Changed("template") {
				cfg.Template = templateName
			}
			if !isSupportedProvider(cfg.Provider) {
				return errors.New("unsupported provider: " + string(cfg.Provider))
			}
			if cfgErr := validateProviderConfig(cfg); cfgErr != nil {
				return cfgErr
			}
			if format != "text" && format != "json" {
				return withExitCode(4, fmt.Errorf("unsupported format %q: must be text or json", format))
			}
			if noUpdateTitle && noUpdateDescription && !postSummary && len(labels) == 0 && len(reviewers) == 0 && !autoLabels {
				return withExitCode(4, errors.New("publish has no remote actions enabled"))
			}
			if cancel := applyRequestTimeout(cmd, cfg); cancel != nil {
				defer cancel()
			}

			diffContent, targetURL, err := resolvePublishDiff(cmd.Context(), cfg, prURL)
			if err != nil {
				return err
			}
			if strings.TrimSpace(diffContent) == "" {
				return withExitCode(3, errors.New("no diff found to publish"))
			}
			summary := summarizeDiff(diffContent, "publish", getModelName(cfg), len(strings.Split(diffContent, "\n")) > 4000)
			diffContent = processDiff(diffContent, 4000)

			systemPrompt, templateErr := NewPromptTemplate(cfg.Template)
			if templateErr != nil {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Warning:", templateErr)
			}

			var title, description string
			eg, egCtx := errgroup.WithContext(cmd.Context())
			eg.Go(func() error {
				var callErr error
				title, callErr = timedCall(cfg, "publish-title", func() (string, error) {
					return chatFn(egCtx, cfg, cfg.Provider, titlePrompt, diffContent)
				})
				return callErr
			})
			eg.Go(func() error {
				var callErr error
				description, callErr = timedCall(cfg, "publish-description", func() (string, error) {
					return chatFn(egCtx, cfg, cfg.Provider, systemPrompt, diffContent)
				})
				return callErr
			})
			if err := eg.Wait(); err != nil {
				return err
			}
			title = strings.TrimSpace(title)
			description = strings.TrimSpace(description)
			if draftIfRisky && publishLooksRisky(description) {
				title = ensureDraftTitle(title)
			}

			appliedLabels := cleanStringList(labels)
			if autoLabels {
				appliedLabels = cleanStringList(append(appliedLabels, derivePublishLabels(summary, description)...))
			}
			reviewers = cleanStringList(reviewers)

			if targetURL == "" {
				targetURL, err = findOrCreatePublishTarget(cmd.Context(), cfg, title)
				if err != nil {
					return err
				}
			}

			updateTitle := !noUpdateTitle
			updateDescription := !noUpdateDescription
			var updateTitleValue *string
			var updateDescriptionValue *string
			if updateTitle {
				updateTitleValue = &title
			}
			if updateDescription {
				body := description
				if !replaceDescription {
					metadata, metaErr := getRemoteMetadata(cmd.Context(), cfg, targetURL)
					if metaErr != nil {
						return metaErr
					}
					body = mergeManagedSection(metadata.Description, description)
				}
				updateDescriptionValue = &body
			}

			if dryRun {
				return writePublishDryRun(cmd, cfg, targetURL, title, description, updateTitle, updateDescription, postSummary, appliedLabels, reviewers)
			}

			if updateTitle || updateDescription {
				if err := updateRemoteMetadata(cmd.Context(), cfg, targetURL, updateTitleValue, updateDescriptionValue); err != nil {
					return err
				}
			}
			if postSummary {
				if err := upsertRemoteManagedComment(cmd.Context(), cfg, targetURL, buildManagedComment(title, description)); err != nil {
					return err
				}
			}
			if len(appliedLabels) > 0 {
				if err := addRemoteLabels(cmd.Context(), cfg, targetURL, appliedLabels); err != nil {
					return err
				}
			}
			if len(reviewers) > 0 {
				if err := requestRemoteReviewers(cmd.Context(), cfg, targetURL, reviewers); err != nil {
					return err
				}
			}

			if format == "json" {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
					URL                string   `json:"url"`
					Title              string   `json:"title"`
					DescriptionUpdated bool     `json:"description_updated"`
					CommentUpserted    bool     `json:"comment_upserted"`
					Labels             []string `json:"labels,omitempty"`
					Reviewers          []string `json:"reviewers,omitempty"`
					Provider           string   `json:"provider"`
					Model              string   `json:"model"`
				}{
					URL:                targetURL,
					Title:              title,
					DescriptionUpdated: updateDescription,
					CommentUpserted:    postSummary,
					Labels:             appliedLabels,
					Reviewers:          reviewers,
					Provider:           string(cfg.Provider),
					Model:              getModelName(cfg),
				})
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Published PR/MR metadata: %s\n", targetURL)
			return nil
		},
	}

	cmd.Flags().StringVar(&prURL, "pr", "", "GitHub PR or GitLab MR URL; omitted means find or create from the current branch")
	cmd.Flags().StringVar(&provider, "provider", "openai", "AI provider to use")
	cmd.Flags().StringVar(&modelOverride, "model", "", "Override the model for this run")
	cmd.Flags().StringVarP(&templateName, "template", "t", "default", "Prompt template to use")
	cmd.Flags().StringVar(&profileName, "profile", "", "Named config profile to activate")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text or json")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview planned remote changes without writing them")
	cmd.Flags().BoolVar(&noUpdateTitle, "no-update-title", false, "Do not update the remote PR/MR title")
	cmd.Flags().BoolVar(&noUpdateDescription, "no-update-description", false, "Do not update the remote PR/MR description/body")
	cmd.Flags().BoolVar(&replaceDescription, "replace-description", false, "Replace the full remote description instead of syncing a managed section")
	cmd.Flags().BoolVar(&postSummary, "post-summary", true, "Create or update a managed PR/MR summary comment")
	cmd.Flags().BoolVar(&autoLabels, "auto-labels", false, "Apply simple labels inferred from the changed files and generated description")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "Label to add to the PR/MR; can be repeated or comma-separated")
	cmd.Flags().StringArrayVar(&reviewers, "reviewer", nil, "Reviewer to request; GitHub uses usernames, GitLab uses numeric user IDs")
	cmd.Flags().BoolVar(&draftIfRisky, "draft-if-risky", false, "Prefix the title with Draft: when generated text indicates high risk")
	_ = cmd.RegisterFlagCompletionFunc("provider", completeValues(providerNames))
	_ = cmd.RegisterFlagCompletionFunc("template", completeValues(templateNames))
	_ = cmd.RegisterFlagCompletionFunc("profile", completeProfiles)
	_ = cmd.RegisterFlagCompletionFunc("format", completeValues([]string{"text", "json"}))
	return cmd
}

func resolvePublishDiff(ctx context.Context, cfg *Config, prURL string) (diffContent, targetURL string, err error) {
	if prURL != "" {
		switch {
		case isGitHubURL(prURL):
			diffContent, err = getPRDiff(ctx, prURL, cfg.GitHubToken, cfg.GitHubBaseURL)
		case isGitLabURL(prURL):
			diffContent, err = getMRDiff(ctx, prURL, cfg.GitLabToken, cfg.GitLabBaseURL)
		default:
			return "", "", fmt.Errorf("unsupported URL %q: must be a GitHub PR (/pull/) or GitLab MR (/-/merge_requests/) URL", prURL)
		}
		return diffContent, prURL, err
	}
	if !isGitRepo() {
		return "", "", errors.New("not a git repository. Pass --pr to publish a remote PR/MR without a local checkout")
	}
	branch, err := getCurrentBranch()
	if err != nil {
		return "", "", fmt.Errorf("could not determine current branch: %w", err)
	}
	diffContent, err = getGitDiff("", false, nil)
	if err != nil {
		return "", "", fmt.Errorf("reading local diff: %w", err)
	}
	if branch != "" {
		diffContent = "Branch: " + branch + "\n\n" + diffContent
	}
	return diffContent, "", nil
}

func findOrCreatePublishTarget(ctx context.Context, cfg *Config, title string) (string, error) {
	branch, err := getCurrentBranch()
	if err != nil {
		return "", fmt.Errorf("could not determine current branch: %w", err)
	}
	if branch == "" {
		return "", errors.New("cannot publish from detached HEAD without --pr")
	}
	remoteURL, err := getRemoteURL()
	if err != nil {
		return "", fmt.Errorf("getting remote URL: %w", err)
	}
	info, err := parseRemoteInfo(remoteURL)
	if err != nil {
		return "", err
	}
	switch {
	case isGitHubHost(info.Host, cfg.GitHubBaseURL):
		if len(info.PathParts) < 2 {
			return "", errors.New("could not parse owner/repo from remote URL")
		}
		return findOrCreateGitHubPRFromConfig(ctx, cfg, info.PathParts[0], info.PathParts[1], branch, title)
	case isGitLabHost(info.Host, cfg.GitLabBaseURL):
		return findOrCreateGitLabMRFromConfig(ctx, cfg, strings.Join(info.PathParts, "/"), branch, title)
	default:
		return "", fmt.Errorf("unrecognised remote host %q; set github_base_url or gitlab_base_url in config", info.Host)
	}
}

func getRemoteMetadata(ctx context.Context, cfg *Config, targetURL string) (prMetadata, error) {
	switch {
	case isGitHubURL(targetURL):
		return getGitHubPRMetadata(ctx, targetURL, cfg.GitHubToken, cfg.GitHubBaseURL)
	case isGitLabURL(targetURL):
		return getGitLabMRMetadata(ctx, targetURL, cfg.GitLabToken, cfg.GitLabBaseURL)
	default:
		return prMetadata{}, fmt.Errorf("unsupported PR/MR URL %q", targetURL)
	}
}

func updateRemoteMetadata(ctx context.Context, cfg *Config, targetURL string, title, description *string) error {
	switch {
	case isGitHubURL(targetURL):
		return updateGitHubPRMetadata(ctx, targetURL, cfg.GitHubToken, cfg.GitHubBaseURL, title, description)
	case isGitLabURL(targetURL):
		return updateGitLabMRMetadata(ctx, targetURL, cfg.GitLabToken, cfg.GitLabBaseURL, title, description)
	default:
		return fmt.Errorf("unsupported PR/MR URL %q", targetURL)
	}
}

func upsertRemoteManagedComment(ctx context.Context, cfg *Config, targetURL, body string) error {
	switch {
	case isGitHubURL(targetURL):
		return upsertGitHubPRComment(ctx, targetURL, cfg.GitHubToken, cfg.GitHubBaseURL, managedCommentMarker, body)
	case isGitLabURL(targetURL):
		return upsertGitLabMRNote(ctx, targetURL, cfg.GitLabToken, cfg.GitLabBaseURL, managedCommentMarker, body)
	default:
		return fmt.Errorf("unsupported PR/MR URL %q", targetURL)
	}
}

func addRemoteLabels(ctx context.Context, cfg *Config, targetURL string, labels []string) error {
	switch {
	case isGitHubURL(targetURL):
		return addGitHubPRLabels(ctx, targetURL, cfg.GitHubToken, cfg.GitHubBaseURL, labels)
	case isGitLabURL(targetURL):
		return addGitLabMRLabels(ctx, targetURL, cfg.GitLabToken, cfg.GitLabBaseURL, labels)
	default:
		return fmt.Errorf("unsupported PR/MR URL %q", targetURL)
	}
}

func requestRemoteReviewers(ctx context.Context, cfg *Config, targetURL string, reviewers []string) error {
	switch {
	case isGitHubURL(targetURL):
		return requestGitHubPRReviewers(ctx, targetURL, cfg.GitHubToken, cfg.GitHubBaseURL, reviewers)
	case isGitLabURL(targetURL):
		return requestGitLabMRReviewers(ctx, targetURL, cfg.GitLabToken, cfg.GitLabBaseURL, reviewers)
	default:
		return fmt.Errorf("unsupported PR/MR URL %q", targetURL)
	}
}

func mergeManagedSection(existing, generated string) string {
	block := managedDescriptionStart + "\n" + strings.TrimSpace(generated) + "\n" + managedDescriptionEnd
	existing = strings.TrimSpace(existing)
	start := strings.Index(existing, managedDescriptionStart)
	end := strings.Index(existing, managedDescriptionEnd)
	if start >= 0 && end > start {
		end += len(managedDescriptionEnd)
		before := strings.TrimSpace(existing[:start])
		after := strings.TrimSpace(existing[end:])
		parts := []string{}
		if before != "" {
			parts = append(parts, before)
		}
		parts = append(parts, block)
		if after != "" {
			parts = append(parts, after)
		}
		return strings.Join(parts, "\n\n")
	}
	if existing == "" {
		return block
	}
	return existing + "\n\n" + block
}

func buildManagedComment(title, description string) string {
	var b strings.Builder
	b.WriteString(managedCommentMarker)
	b.WriteString("\n\n")
	if strings.TrimSpace(title) != "" {
		b.WriteString("## ")
		b.WriteString(strings.TrimSpace(title))
		b.WriteString("\n\n")
	}
	b.WriteString(strings.TrimSpace(description))
	b.WriteByte('\n')
	return b.String()
}

func derivePublishLabels(summary diffSummary, description string) []string {
	labelSet := map[string]bool{}
	lowerDescription := strings.ToLower(description)
	if strings.Contains(lowerDescription, "security") || strings.Contains(lowerDescription, "vulnerab") || strings.Contains(lowerDescription, "credential") {
		labelSet["security"] = true
	}
	if strings.Contains(lowerDescription, "breaking") || strings.Contains(lowerDescription, "migration") {
		labelSet["breaking-change"] = true
	}
	for _, file := range summary.Files {
		path := strings.ToLower(file.Path)
		switch {
		case strings.Contains(path, "test") || strings.HasSuffix(path, "_test.go"):
			labelSet["tests"] = true
		case strings.HasPrefix(path, "docs/") || strings.HasSuffix(path, ".md"):
			labelSet["docs"] = true
		case strings.Contains(path, "docker") || strings.Contains(path, ".github/workflows") || strings.Contains(path, "ci"):
			labelSet["ci"] = true
		case strings.Contains(path, "go.mod") || strings.Contains(path, "go.sum") || strings.Contains(path, "package-lock") || strings.Contains(path, "requirements"):
			labelSet["dependencies"] = true
		}
	}
	labels := make([]string, 0, len(labelSet))
	for label := range labelSet {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	return labels
}

func publishLooksRisky(description string) bool {
	lower := strings.ToLower(description)
	for _, marker := range []string{"breaking", "data loss", "security", "vulnerab", "unsafe", "risk", "fail"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func ensureDraftTitle(title string) string {
	title = strings.TrimSpace(title)
	if strings.HasPrefix(strings.ToLower(title), "draft:") {
		return title
	}
	return "Draft: " + title
}

func writePublishDryRun(cmd *cobra.Command, cfg *Config, targetURL, title, description string, updateTitle, updateDescription, postSummary bool, labels, reviewers []string) error {
	if cmd.Flag("format").Value.String() == "json" {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
			DryRun            bool     `json:"dry_run"`
			URL               string   `json:"url,omitempty"`
			Title             string   `json:"title"`
			DescriptionBytes  int      `json:"description_bytes"`
			WouldUpdateTitle  bool     `json:"would_update_title"`
			WouldUpdateBody   bool     `json:"would_update_description"`
			WouldPostSummary  bool     `json:"would_post_summary"`
			WouldApplyLabels  []string `json:"would_apply_labels,omitempty"`
			WouldAddReviewers []string `json:"would_add_reviewers,omitempty"`
			Provider          string   `json:"provider"`
			Model             string   `json:"model"`
		}{
			DryRun:            true,
			URL:               targetURL,
			Title:             title,
			DescriptionBytes:  len(description),
			WouldUpdateTitle:  updateTitle,
			WouldUpdateBody:   updateDescription,
			WouldPostSummary:  postSummary,
			WouldApplyLabels:  labels,
			WouldAddReviewers: reviewers,
			Provider:          string(cfg.Provider),
			Model:             getModelName(cfg),
		})
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Dry run: no PR/MR metadata, comment, label, or reviewer changes will be written.")
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "- Target: %s\n", targetURL)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "- Title: %s\n", title)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "- Description bytes: %d\n", len(description))
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "- Update title: %v\n", updateTitle)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "- Update description: %v\n", updateDescription)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "- Post summary: %v\n", postSummary)
	if len(labels) > 0 {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "- Labels: %s\n", strings.Join(labels, ", "))
	}
	if len(reviewers) > 0 {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "- Reviewers: %s\n", strings.Join(reviewers, ", "))
	}
	return nil
}

// providerSecrets maps each supported AI provider to the conventional GitHub
// Actions secret name used for its API key.
var providerSecrets = map[string]string{
	"openai":    "OPENAI_API_KEY",
	"anthropic": "ANTHROPIC_API_KEY",
	"gemini":    "GEMINI_API_KEY",
}

// allProviders is the ordered list used by check --all.
var allProviders = []ApiProvider{OpenAI, Anthropic, Gemini, Ollama, ClaudeCLI, GeminiCLI, CodexCLI}

const checkPingPrompt = `Reply with the single word: OK`

// pingResult holds the outcome of a single provider ping.
type pingResult struct {
	provider ApiProvider
	model    string
	elapsed  time.Duration
	err      error
	skipped  bool // true when credentials/binary are absent — not worth pinging
	skipMsg  string
}

// pingProvider sends a minimal prompt to one provider and returns the result.
func pingProvider(ctx context.Context, cfg *Config, provider ApiProvider, chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error)) pingResult {
	provCfg := *cfg
	provCfg.Provider = provider

	// Pre-flight: skip providers whose credentials/binary are clearly absent
	// so we don't waste time on a doomed network call.
	if err := validateProviderConfig(&provCfg); err != nil {
		return pingResult{provider: provider, model: getModelName(&provCfg), skipped: true, skipMsg: err.Error()}
	}

	start := time.Now()
	_, err := chatFn(ctx, &provCfg, provider, checkPingPrompt, "")
	return pingResult{
		provider: provider,
		model:    getModelName(&provCfg),
		elapsed:  time.Since(start),
		err:      err,
	}
}

// newCheckCmd returns a command that validates the configured provider is reachable
// by sending a minimal live ping and reporting the result.
func newCheckCmd(chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error)) *cobra.Command {
	var provider, modelOverride, profileName string
	var all bool

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Validate AI provider access with a live ping",
		Long: `Loads your configuration, prints the resolved settings, and sends a
minimal request to confirm the provider API or CLI is reachable and responding.

Use --all to ping every provider in parallel and print a summary table.

Examples:
  ai-mr-comment check
  ai-mr-comment check --provider anthropic
  ai-mr-comment check --all
  ai-mr-comment check --profile fast`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigForProfile(profileName)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()

			if all {
				if cancel := applyRequestTimeout(cmd, cfg); cancel != nil {
					defer cancel()
				}
				return runCheckAll(cmd.Context(), cfg, chatFn, out)
			}

			if cmd.Flags().Changed("provider") {
				cfg.Provider = ApiProvider(provider)
			}
			if cmd.Flags().Changed("model") {
				setModelOverride(cfg, modelOverride)
			}

			// Print resolved config.
			_, _ = fmt.Fprintf(out, "Provider : %s\n", cfg.Provider)
			_, _ = fmt.Fprintf(out, "Model    : %s\n", getModelName(cfg))
			switch cfg.Provider {
			case OpenAI:
				_, _ = fmt.Fprintf(out, "Endpoint : %s\n", cfg.OpenAIEndpoint)
				_, _ = fmt.Fprintf(out, "API key  : %s\n", maskSecret(cfg.OpenAIAPIKey))
			case Anthropic:
				_, _ = fmt.Fprintf(out, "Endpoint : %s\n", cfg.AnthropicEndpoint)
				_, _ = fmt.Fprintf(out, "API key  : %s\n", maskSecret(cfg.AnthropicAPIKey))
			case Gemini:
				_, _ = fmt.Fprintf(out, "API key  : %s\n", maskSecret(cfg.GeminiAPIKey))
			case Ollama:
				_, _ = fmt.Fprintf(out, "Endpoint : %s\n", cfg.OllamaEndpoint)
			case ClaudeCLI:
				binary, binErr := findClaudeBinary(cfg)
				if binErr != nil {
					_, _ = fmt.Fprintf(out, "Binary   : (not found: %v)\n", binErr)
				} else {
					_, _ = fmt.Fprintf(out, "Binary   : %s\n", binary)
				}
			case GeminiCLI:
				binary, binErr := findGeminiCLIBinary(cfg)
				if binErr != nil {
					_, _ = fmt.Fprintf(out, "Binary   : (not found: %v)\n", binErr)
				} else {
					_, _ = fmt.Fprintf(out, "Binary   : %s\n", binary)
				}
			case CodexCLI:
				binary, binErr := findCodexBinary(cfg)
				if binErr != nil {
					_, _ = fmt.Fprintf(out, "Binary   : (not found: %v)\n", binErr)
				} else {
					_, _ = fmt.Fprintf(out, "Binary   : %s\n", binary)
				}
			}
			_, _ = fmt.Fprintln(out)

			// Validate config before attempting the live call.
			if cfgErr := validateProviderConfig(cfg); cfgErr != nil {
				return fmt.Errorf("config error: %w", cfgErr)
			}
			if cancel := applyRequestTimeout(cmd, cfg); cancel != nil {
				defer cancel()
			}

			_, _ = fmt.Fprintln(out, "Sending ping...")
			start := time.Now()
			reply, callErr := chatFn(cmd.Context(), cfg, cfg.Provider, checkPingPrompt, "")
			elapsed := time.Since(start)

			if callErr != nil {
				_, _ = fmt.Fprintf(out, "FAIL (%dms): %v\n", elapsed.Milliseconds(), callErr)
				return fmt.Errorf("check failed: %w", callErr)
			}

			reply = strings.TrimSpace(reply)
			_, _ = fmt.Fprintf(out, "OK (%dms)\n", elapsed.Milliseconds())
			_, _ = fmt.Fprintf(out, "Response : %s\n", reply)
			return nil
		},
	}

	cmd.Flags().StringVar(&provider, "provider", "", "AI provider to check (overrides config)")
	cmd.Flags().StringVar(&modelOverride, "model", "", "Override the model for this check")
	cmd.Flags().StringVar(&profileName, "profile", "", "Named config profile to activate")
	cmd.Flags().BoolVar(&all, "all", false, "Ping every provider in parallel and print a summary table")
	_ = cmd.RegisterFlagCompletionFunc("provider", completeValues(providerNames))
	_ = cmd.RegisterFlagCompletionFunc("profile", completeProfiles)
	return cmd
}

func newDoctorCmd() *cobra.Command {
	var provider, modelOverride, profileName, presetName, format string

	cmd := &cobra.Command{
		Use:     "doctor",
		Aliases: []string{"config-dump"},
		Short:   "Inspect resolved config and local CLI readiness without a live provider call",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigForProfile(profileName)
			if err != nil {
				return err
			}
			dummyExitCode := false
			dummyPlain := false
			dummyTitle := false
			if _, presetErr := applyRootPreset(cmd, presetName, cfg, &format, &dummyExitCode, &dummyPlain, &dummyTitle); presetErr != nil {
				return withExitCode(4, presetErr)
			}
			if cmd.Flags().Changed("provider") {
				cfg.Provider = ApiProvider(provider)
			}
			if cmd.Flags().Changed("model") {
				setModelOverride(cfg, modelOverride)
			}
			if !isSupportedProvider(cfg.Provider) {
				return errors.New("unsupported provider: " + string(cfg.Provider))
			}
			if format != "text" && format != "json" {
				return fmt.Errorf("unsupported format %q: must be text or json", format)
			}

			type doctorPayload struct {
				ConfigFile string            `json:"config_file"`
				Profile    string            `json:"profile,omitempty"`
				Preset     string            `json:"preset,omitempty"`
				Provider   string            `json:"provider"`
				Model      string            `json:"model"`
				Template   string            `json:"template"`
				Timeout    string            `json:"request_timeout"`
				Git        map[string]string `json:"git"`
				Secrets    map[string]string `json:"secrets"`
				Binaries   map[string]string `json:"binaries"`
			}

			gitInfo := map[string]string{"repository": "false"}
			if isGitRepo() {
				gitInfo["repository"] = "true"
				if branch, branchErr := getCurrentBranch(); branchErr == nil && branch != "" {
					gitInfo["branch"] = branch
				}
				if remote, remoteErr := getRemoteURL(); remoteErr == nil && remote != "" {
					gitInfo["remote"] = sanitizeRemoteURL(remote)
				}
			}
			binaries := map[string]string{}
			if binary, binErr := findClaudeBinary(cfg); binErr == nil {
				binaries["claude-cli"] = binary
			} else {
				binaries["claude-cli"] = "(not found)"
			}
			if binary, binErr := findGeminiCLIBinary(cfg); binErr == nil {
				binaries["gemini-cli"] = binary
			} else {
				binaries["gemini-cli"] = "(not found)"
			}
			if binary, binErr := findCodexBinary(cfg); binErr == nil {
				binaries["codex-cli"] = binary
			} else {
				binaries["codex-cli"] = "(not found)"
			}
			configFile := cfg.ConfigFile
			if configFile == "" {
				configFile = "(none)"
			}
			payload := doctorPayload{
				ConfigFile: configFile,
				Profile:    profileName,
				Preset:     presetName,
				Provider:   string(cfg.Provider),
				Model:      getModelName(cfg),
				Template:   cfg.Template,
				Timeout:    cfg.RequestTimeout.String(),
				Git:        gitInfo,
				Secrets: map[string]string{
					"OPENAI_API_KEY":    secretStatus(cfg.OpenAIAPIKey),
					"ANTHROPIC_API_KEY": secretStatus(cfg.AnthropicAPIKey),
					"GEMINI_API_KEY":    secretStatus(cfg.GeminiAPIKey),
					"GITHUB_TOKEN":      secretStatus(cfg.GitHubToken),
					"GITLAB_TOKEN":      secretStatus(cfg.GitLabToken),
				},
				Binaries: binaries,
			}
			out := cmd.OutOrStdout()
			if format == "json" {
				return json.NewEncoder(out).Encode(payload)
			}
			_, _ = fmt.Fprintf(out, "Config file : %s\n", payload.ConfigFile)
			if profileName != "" {
				_, _ = fmt.Fprintf(out, "Profile     : %s\n", profileName)
			}
			if presetName != "" {
				_, _ = fmt.Fprintf(out, "Preset      : %s\n", presetName)
			}
			_, _ = fmt.Fprintf(out, "Provider    : %s\n", payload.Provider)
			_, _ = fmt.Fprintf(out, "Model       : %s\n", payload.Model)
			_, _ = fmt.Fprintf(out, "Template    : %s\n", payload.Template)
			_, _ = fmt.Fprintf(out, "Timeout     : %s\n", payload.Timeout)
			_, _ = fmt.Fprintf(out, "Git repo    : %s\n", payload.Git["repository"])
			if payload.Git["branch"] != "" {
				_, _ = fmt.Fprintf(out, "Git branch  : %s\n", payload.Git["branch"])
			}
			_, _ = fmt.Fprintln(out, "\nSecrets:")
			for _, name := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GEMINI_API_KEY", "GITHUB_TOKEN", "GITLAB_TOKEN"} {
				_, _ = fmt.Fprintf(out, "- %s: %s\n", name, payload.Secrets[name])
			}
			_, _ = fmt.Fprintln(out, "\nCLI binaries:")
			for _, name := range []string{"claude-cli", "gemini-cli", "codex-cli"} {
				_, _ = fmt.Fprintf(out, "- %s: %s\n", name, payload.Binaries[name])
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "", "AI provider to inspect (overrides config)")
	cmd.Flags().StringVar(&modelOverride, "model", "", "Override the model for inspection")
	cmd.Flags().StringVar(&profileName, "profile", "", "Named config profile to activate")
	cmd.Flags().StringVar(&presetName, "preset", "", "Preset defaults: ci, local-fast, security, release-notes")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text or json")
	_ = cmd.RegisterFlagCompletionFunc("provider", completeValues(providerNames))
	_ = cmd.RegisterFlagCompletionFunc("profile", completeProfiles)
	_ = cmd.RegisterFlagCompletionFunc("preset", completeValues(presetNames))
	_ = cmd.RegisterFlagCompletionFunc("format", completeValues([]string{"text", "json"}))
	return cmd
}

func secretStatus(value string) string {
	if value == "" {
		return "missing"
	}
	return "set"
}

// runCheckAll pings all providers concurrently and prints a summary table.
// Returns an error if any configured (non-skipped) provider fails.
func runCheckAll(ctx context.Context, cfg *Config, chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error), out io.Writer) error {
	_, _ = fmt.Fprintln(out, "Pinging all providers...")
	_, _ = fmt.Fprintln(out)

	type indexedResult struct {
		idx int
		pingResult
	}

	results := make([]pingResult, len(allProviders))
	ch := make(chan indexedResult, len(allProviders))

	for i, p := range allProviders {
		i, p := i, p
		go func() {
			ch <- indexedResult{idx: i, pingResult: pingProvider(ctx, cfg, p, chatFn)}
		}()
	}
	for range allProviders {
		r := <-ch
		results[r.idx] = r.pingResult
	}

	// Print aligned table.
	const colProvider = 12
	const colModel = 24
	_, _ = fmt.Fprintf(out, "%-*s  %-*s  %s\n", colProvider, "PROVIDER", colModel, "MODEL", "STATUS")
	_, _ = fmt.Fprintf(out, "%s  %s  %s\n", strings.Repeat("-", colProvider), strings.Repeat("-", colModel), strings.Repeat("-", 20))

	var anyFailed bool
	for _, r := range results {
		var status string
		switch {
		case r.skipped:
			status = "SKIP — " + firstLine(r.skipMsg)
		case r.err != nil:
			anyFailed = true
			status = fmt.Sprintf("FAIL (%dms) — %s", r.elapsed.Milliseconds(), firstLine(r.err.Error()))
		default:
			status = fmt.Sprintf("OK   (%dms)", r.elapsed.Milliseconds())
		}
		_, _ = fmt.Fprintf(out, "%-*s  %-*s  %s\n", colProvider, r.provider, colModel, r.model, status)
	}
	if anyFailed {
		_, _ = fmt.Fprintf(out, "  tip: run 'check --provider <name>' for full error details\n")
	}

	_, _ = fmt.Fprintln(out)
	if anyFailed {
		return errors.New("one or more providers failed — see table above")
	}
	return nil
}

// firstLine returns the text up to the first newline, trimmed, and truncated
// to maxLen runes with "…" appended when exceeded.
func firstLine(s string) string {
	const maxLen = 72
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len([]rune(s)) > maxLen {
		return string([]rune(s)[:maxLen]) + "…"
	}
	return s
}

// maskSecret returns the first 4 characters of s followed by "****", or
// "(not set)" when s is empty.
func maskSecret(s string) string {
	if s == "" {
		return "(not set)"
	}
	if len(s) <= 4 {
		return "****"
	}
	return s[:4] + "****"
}

// newGenWorkflowCmd returns a command that generates a GitHub Actions workflow
// file that automatically runs ai-mr-comment on every pull request.
func newGenWorkflowCmd() *cobra.Command {
	var provider, outputPath string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "gen-workflow",
		Short: "Generate a GitHub Actions workflow for automatic AI PR review",
		Long: `Writes a GitHub Actions workflow YAML file that automatically runs
ai-mr-comment on every pull request (opened, synchronised, reopened) and
posts the AI review as a PR comment.

Use --output=- to print to stdout instead of writing a file.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			secretName, ok := providerSecrets[provider]
			if !ok {
				supported := []string{"openai", "anthropic", "gemini"}
				return fmt.Errorf("unsupported provider %q for gen-workflow; supported: %s", provider, strings.Join(supported, ", "))
			}
			providerEnvVar := secretName // env var name == secret name

			workflow := fmt.Sprintf(`name: AI PR Review

on:
  pull_request:
    types: [opened, synchronize, reopened]

permissions:
  pull-requests: write
  contents: read

jobs:
  ai-review:
    runs-on: ubuntu-latest
    steps:
      - name: Install ai-mr-comment
        run: |
          curl -fsSL https://github.com/pbsladek/ai-mr-comment/releases/latest/download/ai-mr-comment-linux-amd64 \
            -o /usr/local/bin/ai-mr-comment
          chmod +x /usr/local/bin/ai-mr-comment

      - name: Generate and post AI review
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          %s: ${{ secrets.%s }}
        run: |
          ai-mr-comment \
            --pr "${{ github.event.pull_request.html_url }}" \
            --provider %s \
            --post
`, providerEnvVar, secretName, provider)

			if outputPath == "-" {
				_, _ = fmt.Fprint(cmd.OutOrStdout(), workflow)
				return nil
			}

			if dryRun {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Dry run: would write workflow to %s using provider %s and secret %s\n", outputPath, provider, secretName)
				return nil
			}

			if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
				return fmt.Errorf("creating output directory: %w", err)
			}
			if err := os.WriteFile(outputPath, []byte(workflow), 0o600); err != nil {
				return fmt.Errorf("writing workflow file: %w", err)
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Wrote workflow to %s\n", outputPath)
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Add secret %q to your repo, then commit the workflow file.\n", secretName)
			return nil
		},
	}

	cmd.Flags().StringVar(&provider, "provider", "openai", "AI provider to use in the workflow (openai, anthropic, gemini)")
	cmd.Flags().StringVar(&outputPath, "output", ".github/workflows/ai-review.yml", "Output file path (use - for stdout)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would be written without creating files")
	_ = cmd.RegisterFlagCompletionFunc("provider", completeValues([]string{"openai", "anthropic", "gemini"}))
	return cmd
}

// aliasBlock is the shell snippet printed by gen-aliases.
// It is a Go constant so tests can verify the exact output.
const aliasBlock = `# ai-mr-comment v1 aliases
# Generated by: ai-mr-comment gen-aliases
# Add to your ~/.bashrc or ~/.zshrc, then reload with: source ~/.bashrc

alias amc='ai-mr-comment'                                      # main command
alias amc-staged='ai-mr-comment --staged'                      # review staged changes
alias amc-commit='ai-mr-comment --commit-msg --staged'         # generate commit message
alias amc-commit-multi='ai-mr-comment --commit-msg --multi-line --staged'  # multi-line commit (pre-fills PR/MR)
alias amc-title='ai-mr-comment --title'                        # include a PR/MR title
alias amc-json='ai-mr-comment --format=json'                   # JSON output
alias amc-debug='ai-mr-comment --debug'                        # token/cost estimate
alias amc-chaos='ai-mr-comment --chaos'                        # chaotic but accurate MR/PR description
alias amc-haiku='ai-mr-comment --haiku'                        # MR/PR description as haikus
alias amc-roast='ai-mr-comment --roast'                        # sardonically judgmental MR/PR description
alias amc-intern='ai-mr-comment --intern'                      # overly enthusiastic junior-dev MR/PR description
alias amc-shakespeare='ai-mr-comment --shakespeare'            # MR/PR description in Shakespearean English
alias amc-manager='ai-mr-comment --manager'                    # passive-aggressive corporate MR/PR description
alias amc-yoda='ai-mr-comment --yoda'                          # MR/PR description in Yoda syntax
alias amc-excuse='ai-mr-comment --excuse'                      # technically accurate MR/PR description with excuses
alias amc-conventional='ai-mr-comment --template=conventional' # conventional commits style MR/PR description
alias amc-emoji='ai-mr-comment --template=emoji'               # emoji-rich MR/PR description
alias amc-jira='ai-mr-comment --template=jira'                 # Jira-friendly MR/PR description
alias amc-monday='ai-mr-comment --template=monday'             # Monday.com task-style MR/PR description
alias amc-sassy='ai-mr-comment --template=sassy'               # sassy MR/PR description
alias amc-technical='ai-mr-comment --template=technical'       # deeply technical MR/PR description
alias amc-user='ai-mr-comment --template=user-focused'         # user-impact focused MR/PR description
alias amc-qc='ai-mr-comment quick-commit'                      # stage + AI commit + push
alias amc-qc-dry='ai-mr-comment quick-commit --dry-run'        # preview commit msg
alias amc-qc-edit='ai-mr-comment quick-commit --edit'          # edit generated commit msg before committing
alias amc-qc-local='ai-mr-comment quick-commit --no-push'      # commit locally without pushing
alias amc-qc-tracked='ai-mr-comment quick-commit --tracked-only'  # commit tracked changes only
alias amc-qc-signoff='ai-mr-comment quick-commit --signoff'    # append Signed-off-by trailer
alias amc-qc-fix='ai-mr-comment quick-commit --type=fix'       # force fix commit type
alias amc-qc-docs='ai-mr-comment quick-commit --type=docs'     # force docs commit type
alias amc-qc-detailed='ai-mr-comment quick-commit --message-template=detailed'  # detailed commit body
alias amc-qc-release='ai-mr-comment quick-commit --message-template=release'    # release-note-ready commit body
alias amc-qc-ticket='ai-mr-comment quick-commit --message-template=ticket'       # ticket-oriented commit body
alias amc-qc-breaking='ai-mr-comment quick-commit --breaking'  # breaking change commit (feat!)
alias amc-qc-chaos='ai-mr-comment quick-commit --chaos'              # funny/absurd conventional commit
alias amc-qc-haiku='ai-mr-comment quick-commit --haiku'              # commit description as a haiku
alias amc-qc-roast='ai-mr-comment quick-commit --roast'              # passive-aggressive accurate commit
alias amc-qc-fortune='ai-mr-comment quick-commit --fortune'          # commit + dev-wisdom fortune trailer
alias amc-qc-monday='ai-mr-comment quick-commit --monday'            # low-energy Monday-morning commit
alias amc-qc-jira='ai-mr-comment quick-commit --jira'                # commit prefixed with Jira ticket key
alias amc-qc-emoji='ai-mr-comment quick-commit --emoji-commit'       # commit with type-matched gitmoji
alias amc-qc-sassy='ai-mr-comment quick-commit --sassy'              # sassy but accurate commit
alias amc-qc-technical='ai-mr-comment quick-commit --technical'      # maximum technical precision commit
alias amc-qc-intern='ai-mr-comment quick-commit --intern'            # overly enthusiastic junior-dev commit
alias amc-qc-shakespeare='ai-mr-comment quick-commit --shakespeare'  # commit description in Shakespearean English
alias amc-qc-manager='ai-mr-comment quick-commit --manager'          # passive-aggressive corporate commit
alias amc-qc-yoda='ai-mr-comment quick-commit --yoda'                # commit description in Yoda syntax
alias amc-qc-excuse='ai-mr-comment quick-commit --excuse'            # accurate commit with built-in excuse
alias amc-cl='ai-mr-comment changelog'                         # generate changelog entry
alias amc-models='ai-mr-comment models'                        # list available models
alias amc-init='ai-mr-comment init-config'                     # write default config
`

// newGenAliasesCmd returns the gen-aliases subcommand, which prints a shell
// alias block for ai-mr-comment to stdout. Users source the output into their
// shell profile to get short amc-* aliases.
func newGenAliasesCmd() *cobra.Command {
	var shell, outputPath string

	cmd := &cobra.Command{
		Use:   "gen-aliases",
		Short: "Print shell aliases for ai-mr-comment (amc and amc-*)",
		Long: `Prints a block of shell alias definitions to stdout.

Add them to your shell profile with one of:

  # Append once:
  ai-mr-comment gen-aliases >> ~/.bashrc   # or ~/.zshrc

  # Re-generate on every shell start (always up-to-date):
  eval "$(ai-mr-comment gen-aliases)"

Aliases defined:
  amc                — shorthand for ai-mr-comment
  amc-staged         — review staged changes
  amc-commit         — generate a commit message (--commit-msg --staged)
  amc-commit-multi   — multi-line commit message that pre-fills PR/MR title+description
  amc-title          — generate comment + PR/MR title
  amc-json           — output as JSON
  amc-debug          — show token/cost estimate
  amc-chaos          — chaotic but accurate MR/PR description
  amc-haiku          — MR/PR description as haikus
  amc-roast          — sardonically judgmental MR/PR description
  amc-intern         — overly enthusiastic junior-dev MR/PR description
  amc-shakespeare    — MR/PR description in Shakespearean English
  amc-manager        — passive-aggressive corporate MR/PR description
  amc-yoda           — MR/PR description in Yoda syntax
  amc-excuse         — technically accurate MR/PR description with excuses
  amc-conventional   — conventional commits style MR/PR description
  amc-emoji          — emoji-rich MR/PR description
  amc-jira           — Jira-friendly MR/PR description
  amc-monday         — Monday.com task-style MR/PR description
  amc-sassy          — sassy MR/PR description
  amc-technical      — deeply technical MR/PR description
  amc-user           — user-impact focused MR/PR description
  amc-qc             — quick-commit (stage + AI commit + push)
  amc-qc-dry         — quick-commit dry-run (preview only)
  amc-qc-edit        — quick-commit with editor review before commit
  amc-qc-local       — quick-commit without pushing
  amc-qc-tracked     — quick-commit tracked changes only
  amc-qc-signoff     — quick-commit with Signed-off-by trailer
  amc-qc-fix         — quick-commit forcing fix type
  amc-qc-docs        — quick-commit forcing docs type
  amc-qc-detailed    — quick-commit with detailed message template
  amc-qc-release     — quick-commit with release-note-ready message template
  amc-qc-ticket      — quick-commit with ticket-oriented message template
  amc-qc-breaking    — quick-commit with breaking change (feat!)
  amc-qc-chaos       — quick-commit with funny/absurd conventional commit
  amc-qc-haiku       — quick-commit with commit description as a haiku
  amc-qc-roast       — quick-commit with passive-aggressive accurate commit
  amc-qc-fortune     — quick-commit with dev-wisdom fortune trailer
  amc-qc-monday      — quick-commit with casual Monday-morning tone
  amc-qc-jira        — quick-commit prefixed with Jira ticket key from branch
  amc-qc-emoji       — quick-commit with type-matched gitmoji appended
  amc-qc-sassy       — quick-commit with sassy but accurate message
  amc-qc-technical   — quick-commit with maximum technical precision
  amc-qc-intern      — quick-commit as an overly enthusiastic junior dev
  amc-qc-shakespeare — quick-commit description in Shakespearean English
  amc-qc-manager     — quick-commit in passive-aggressive corporate speak
  amc-qc-yoda        — quick-commit description in Yoda syntax
  amc-qc-excuse      — quick-commit with a built-in excuse
  amc-cl             — changelog subcommand
  amc-models         — list available models
  amc-init           — write default config file`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if shell != "bash" && shell != "zsh" {
				return fmt.Errorf("unsupported shell %q: must be bash or zsh", shell)
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprint(out, aliasBlock)

			if outputPath != "" {
				return os.WriteFile(outputPath, []byte(aliasBlock), 0600) //nolint:gosec // G306: user-owned shell config file
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&shell, "shell", "bash", "Target shell: bash or zsh (both use the same alias syntax)")
	cmd.Flags().StringVar(&outputPath, "output", "", "Also write aliases to this file (e.g. ~/.bashrc)")
	_ = cmd.RegisterFlagCompletionFunc("shell", completeValues([]string{"bash", "zsh"}))
	return cmd
}
