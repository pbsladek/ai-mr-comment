package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
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
