package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed templates/default.tmpl
var defaultPromptTemplate string

// titlePrompt is the system prompt used when --title is set. It instructs the
// model to produce only a single concise title line with no extra text.
const titlePrompt = `Generate a single-line MR/PR title for the following diff.
Output only the title text — no explanation, no punctuation at the end, no quotes.
Keep it under 72 characters. Use the imperative mood (e.g. "Add", "Fix", "Refactor").
If the active template follows Conventional Commits style, prefix with the appropriate type (feat, fix, chore, etc.).`

// commitMsgPrompt is the system prompt used when --commit-msg is set. It
// instructs the model to produce a single conventional-style commit message
// with no extra text, suitable for use in git commit -m "...".
const commitMsgPrompt = `Generate a single-line git commit message for the following diff.
Output only the commit message — no explanation, no quotes, no trailing punctuation.
Keep it under 72 characters. Use the imperative mood (e.g. "Add", "Fix", "Refactor").
Follow Conventional Commits format: type(scope): description
Valid types: feat, fix, docs, style, refactor, test, chore, perf, ci, build
The scope is optional; omit it if it would be too broad or redundant.
Example: feat(auth): add JWT refresh token support`

// changelogPrompt is the system prompt used by the changelog subcommand.
// It instructs the model to produce a user-facing changelog entry in
// Keep a Changelog markdown format, grouped by change type.
const changelogPrompt = `You are writing a user-facing changelog entry for a software release.
Analyse the provided git diff and produce a changelog section in Keep a Changelog format.

Rules:
- Output ONLY the changelog markdown — no preamble, no explanation, no code fences.
- Group changes under the appropriate headings (use only the headings that apply):
  ### Added      — new features visible to end users
  ### Changed    — changes to existing behaviour
  ### Deprecated — features that will be removed in a future release
  ### Removed    — features removed in this release
  ### Fixed      — bug fixes
  ### Security   — security-related fixes or improvements
  ### Breaking Changes — backwards-incompatible changes (API, CLI flags, config keys)
- Each item is a single bullet (- …) in plain English aimed at end users, not developers.
- Omit internal refactors, test changes, and formatting-only changes unless they affect behaviour.
- If nothing fits a heading, omit that heading entirely.
- Keep each bullet concise (one sentence, under 100 characters).

Example output:
### Added
- Users can now export results as CSV from the dashboard.

### Fixed
- Sorted list no longer loses the selected item after refresh.

### Breaking Changes
- The --output flag now requires an explicit file extension.`

// resolveSystemPrompt interprets the value of a --system-prompt flag.
//
// Three forms are accepted:
//
//	""           — no override; the caller should use its default prompt.
//	"@path"      — read the prompt from the file at path (stripped of the leading @).
//	"any text"   — use the value as the prompt verbatim.
//
// An error is returned only when @file syntax is used and the file cannot be read.
func resolveSystemPrompt(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if strings.HasPrefix(raw, "@") {
		path := raw[1:]
		content, err := os.ReadFile(path) //nolint:gosec // G304: reading user-supplied prompt file is intentional
		if err != nil {
			return "", fmt.Errorf("--system-prompt: cannot read file %q: %w", path, err)
		}
		return strings.TrimSpace(string(content)), nil
	}
	return raw, nil
}

// NewPromptTemplate returns the system prompt for the given template name.
// For "default" it returns the embedded template. For any other name it
// searches ./templates/<name>.tmpl, ./<name>.tmpl, and
// ~/.config/ai-mr-comment/templates/<name>.tmpl, falling back to the default
// if none are found.
func NewPromptTemplate(templateName string) (string, error) {
	if templateName == "default" {
		return defaultPromptTemplate, nil
	}

	templateFileName := templateName + ".tmpl"
	searchPaths := []string{
		filepath.Join(".", "templates", templateFileName),
		filepath.Join(".", templateFileName),
	}
	if home, err := os.UserHomeDir(); err == nil {
		searchPaths = append(searchPaths, filepath.Join(home, ".config", "ai-mr-comment", "templates", templateFileName))
	}

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			content, err := os.ReadFile(path) //nolint:gosec // G304: reading user-configured prompt template file is intentional
			if err != nil {
				return defaultPromptTemplate, fmt.Errorf("failed to read template %s, falling back to default: %w", path, err)
			}
			return string(content), nil
		}
	}

	return defaultPromptTemplate, fmt.Errorf("template '%s' not found, falling back to default", templateName)
}
