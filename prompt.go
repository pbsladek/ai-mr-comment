package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed templates/default.tmpl
var defaultPromptTemplate string

func NewPromptTemplate(templateName string) (string, error) {
	if templateName == "default" {
		return defaultPromptTemplate, nil
	}

	// Look for template in current directory, then home directory
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
			content, err := os.ReadFile(path)
			if err != nil {
				return defaultPromptTemplate, fmt.Errorf("failed to read template %s, falling back to default: %w", path, err)
			}
			return string(content), nil
		}
	}

	return defaultPromptTemplate, fmt.Errorf("template '%s' not found, falling back to default", templateName)
}
