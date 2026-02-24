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

//go:embed templates/commit-msg.tmpl
var commitMsgPrompt string

//go:embed templates/quick-commit.tmpl
var quickCommitPrompt string

//go:embed templates/quick-commit-free.tmpl
var quickCommitFreePrompt string

//go:embed templates/commit-msg-body.tmpl
var commitMsgBodyPrompt string

//go:embed templates/changelog.tmpl
var changelogPrompt string

// titlePrompt is the system prompt used when --title is set. It instructs the
// model to produce only a single concise title line with no extra text.
const titlePrompt = `Generate a single-line MR/PR title for the following diff.
Output only the title text — no explanation, no punctuation at the end, no quotes.
Keep it under 72 characters. Use the imperative mood (e.g. "Add", "Fix", "Refactor").
If the active template follows Conventional Commits style, prefix with the appropriate type (feat, fix, chore, etc.).`

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
