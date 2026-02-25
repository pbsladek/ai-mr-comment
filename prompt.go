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

//go:embed templates/quick-commit-chaos.tmpl
var quickCommitChaosPrompt string

//go:embed templates/quick-commit-haiku.tmpl
var quickCommitHaikuPrompt string

//go:embed templates/quick-commit-roast.tmpl
var quickCommitRoastPrompt string

//go:embed templates/quick-commit-monday.tmpl
var quickCommitMondayPrompt string

//go:embed templates/quick-commit-jira.tmpl
var quickCommitJiraPrompt string

//go:embed templates/quick-commit-emoji.tmpl
var quickCommitEmojiPrompt string

//go:embed templates/quick-commit-sassy.tmpl
var quickCommitSassyPrompt string

//go:embed templates/quick-commit-technical.tmpl
var quickCommitTechnicalPrompt string

//go:embed templates/quick-commit-intern.tmpl
var quickCommitInternPrompt string

//go:embed templates/quick-commit-shakespeare.tmpl
var quickCommitShakespearePrompt string

//go:embed templates/quick-commit-manager.tmpl
var quickCommitManagerPrompt string

//go:embed templates/quick-commit-yoda.tmpl
var quickCommitYodaPrompt string

//go:embed templates/quick-commit-excuse.tmpl
var quickCommitExcusePrompt string

//go:embed templates/commit-msg-body.tmpl
var commitMsgBodyPrompt string

//go:embed templates/changelog.tmpl
var changelogPrompt string

//go:embed templates/mr-chaos.tmpl
var mrChaosPrompt string

//go:embed templates/mr-haiku.tmpl
var mrHaikuPrompt string

//go:embed templates/mr-roast.tmpl
var mrRoastPrompt string

//go:embed templates/mr-intern.tmpl
var mrInternPrompt string

//go:embed templates/mr-shakespeare.tmpl
var mrShakespearePrompt string

//go:embed templates/mr-manager.tmpl
var mrManagerPrompt string

//go:embed templates/mr-yoda.tmpl
var mrYodaPrompt string

//go:embed templates/mr-excuse.tmpl
var mrExcusePrompt string

// fortunePrompt is used to generate a short developer-wisdom fortune to append
// as a commit message trailer when --fortune is set.
const fortunePrompt = `Generate a single short fortune-cookie-style quote for a software developer.
Output ONLY the quote — no attribution, no explanation, no quotes around it, no code fences.
Keep it under 80 characters. It should be witty, wise, or gently humorous.
Draw from themes like debugging, shipping, complexity, naming things, or the nature of code.
Generate something original every time.

Examples of the spirit (do NOT copy literally):
  The best code is the code you didn't have to write.
  It works on my machine is not a deployment strategy.
  Naming things is hard. Naming things well is an art.
  Every comment is an apology for unclear code.
  Ship it. The bugs will tell you what to fix next.`

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
