package prompts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveSystemPrompt interprets the value of a --system-prompt flag.
//
// Three forms are accepted:
//
//	""           - no override; the caller should use its default prompt.
//	"@path"      - read the prompt from the file at path (stripped of the leading @).
//	"any text"   - use the value as the prompt verbatim.
//
// An error is returned only when @file syntax is used and the file cannot be read.
func ResolveSystemPrompt(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if path, ok := strings.CutPrefix(raw, "@"); ok {
		content, err := os.ReadFile(path) //nolint:gosec // G304: reading user-supplied prompt file is intentional
		if err != nil {
			return "", fmt.Errorf("--system-prompt: cannot read file %q: %w", path, err)
		}
		return strings.TrimSpace(string(content)), nil
	}
	return raw, nil
}

// FindCustomTemplate searches supported custom template locations and returns
// the first matching template body.
func FindCustomTemplate(templateName string) (string, error) {
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
				return "", fmt.Errorf("failed to read template %s, falling back to default: %w", path, err)
			}
			return string(content), nil
		}
	}

	return "", fmt.Errorf("template '%s' not found, falling back to default", templateName)
}
