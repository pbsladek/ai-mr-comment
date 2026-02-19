package main

import (
	"os"
	"strings"
	"testing"
)

func TestNewPromptTemplate_Default(t *testing.T) {
	prompt, err := NewPromptTemplate("default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(prompt, "## Key Changes") {
		t.Error("default template should contain ## Key Changes")
	}
}

func TestNewPromptTemplate_CustomFile(t *testing.T) {
	// Create a temporary template file
	dir := t.TempDir()
	templatePath := dir + "/templates"
	if err := os.Mkdir(templatePath, 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	customTmpl := "This is a custom template."
	err := os.WriteFile(templatePath+"/custom.tmpl", []byte(customTmpl), 0644)
	if err != nil {
		t.Fatalf("failed to write custom template: %v", err)
	}

	// Change to the temp directory so the template can be found
	origWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}
	defer func() { _ = os.Chdir(origWD) }()

	prompt, err := NewPromptTemplate("custom")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompt != customTmpl {
		t.Errorf("expected '%s', got '%s'", customTmpl, prompt)
	}
}

func TestNewPromptTemplate_NotFound(t *testing.T) {
	_, err := NewPromptTemplate("nonexistent")
	if err == nil {
		t.Fatal("expected an error for a nonexistent template, but got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected error to contain 'not found', got '%v'", err)
	}
}
