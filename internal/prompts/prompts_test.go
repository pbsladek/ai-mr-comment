package prompts

import (
	"os"
	"strings"
	"testing"
)

func TestResolveSystemPrompt(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got, err := ResolveSystemPrompt("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "" {
			t.Fatalf("expected empty prompt, got %q", got)
		}
	})

	t.Run("inline", func(t *testing.T) {
		got, err := ResolveSystemPrompt("Focus only on security issues.")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "Focus only on security issues." {
			t.Fatalf("unexpected prompt: %q", got)
		}
	})

	t.Run("file", func(t *testing.T) {
		f, err := os.CreateTemp("", "prompt-*.txt")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Remove(f.Name()) }()
		_, _ = f.WriteString("  Custom prompt from file.  ")
		_ = f.Close()

		got, err := ResolveSystemPrompt("@" + f.Name())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "Custom prompt from file." {
			t.Fatalf("expected trimmed file content, got %q", got)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := ResolveSystemPrompt("@/does/not/exist.txt")
		if err == nil || !strings.Contains(err.Error(), "cannot read file") {
			t.Fatalf("expected file-read error, got %v", err)
		}
	})
}

func TestFindCustomTemplate(t *testing.T) {
	dir := t.TempDir()
	templatePath := dir + "/templates"
	if err := os.Mkdir(templatePath, 0o755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	customTmpl := "This is a custom template."
	if err := os.WriteFile(templatePath+"/custom.tmpl", []byte(customTmpl), 0o644); err != nil {
		t.Fatalf("failed to write custom template: %v", err)
	}

	origWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}
	defer func() { _ = os.Chdir(origWD) }()

	got, err := FindCustomTemplate("custom")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != customTmpl {
		t.Fatalf("expected %q, got %q", customTmpl, got)
	}
}

func TestFindCustomTemplate_NotFound(t *testing.T) {
	dir := t.TempDir()
	origWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}
	defer func() { _ = os.Chdir(origWD) }()

	_, err := FindCustomTemplate("nonexistent")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestNewTemplateBuiltinsAndFallback(t *testing.T) {
	if got, err := NewTemplate("default"); err != nil || got != DefaultTemplate {
		t.Fatalf("default template = %q, %v", got, err)
	}
	for name, want := range BuiltinTemplates {
		got, err := NewTemplate(name)
		if err != nil {
			t.Fatalf("NewTemplate(%q) failed: %v", name, err)
		}
		if got != want {
			t.Fatalf("NewTemplate(%q) mismatch", name)
		}
	}

	dir := t.TempDir()
	if err := os.Mkdir(dir+"/templates", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/templates/custom.tmpl", []byte("custom body"), 0o644); err != nil {
		t.Fatal(err)
	}
	origWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWD) }()

	got, err := NewTemplate("custom")
	if err != nil || got != "custom body" {
		t.Fatalf("custom template = %q, %v", got, err)
	}
	got, err = NewTemplate("missing")
	if err == nil || got != DefaultTemplate {
		t.Fatalf("missing template should return default and error, got %q, %v", got, err)
	}
}
