package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestLoadConfig_WithValidTOML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".ai-mr-comment.toml")

	content := `
provider = "anthropic"
openai_model = "gpt-4"
anthropic_model = "claude-3-opus"
`

	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("toml")

	cfg, err := loadConfigWith(v, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Provider != Anthropic {
		t.Errorf("expected provider Anthropic, got %v", cfg.Provider)
	}
	if cfg.OpenAIModel != "gpt-4" {
		t.Errorf("expected OpenAI model gpt-4, got %v", cfg.OpenAIModel)
	}
	if cfg.AnthropicModel != "claude-3-opus" {
		t.Errorf("expected Anthropic model claude-3-opus, got %v", cfg.AnthropicModel)
	}
}

func TestLoadConfig_DefaultsWhenMissingFile(t *testing.T) {
	v := viper.New()
	v.SetConfigName(".ai-mr-comment.toml")
	v.SetConfigType("toml")
	v.AddConfigPath("/nonexistent") // a directory that doesn't exist

	cfg, err := loadConfigWith(v, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Provider != Anthropic {
		t.Errorf("expected provider Anthropic, got %v", cfg.Provider)
	}
	if cfg.OpenAIModel != "gpt-4.1-mini" {
		t.Errorf("expected OpenAI model gpt-4.1-mini, got %v", cfg.OpenAIModel)
	}
	if cfg.AnthropicModel != "claude-sonnet-4-6" {
		t.Errorf("expected Anthropic model claude-sonnet-4-6, got %v", cfg.AnthropicModel)
	}
	if cfg.OllamaModel != "llama3.2" {
		t.Errorf("expected Ollama model llama3.2, got %v", cfg.OllamaModel)
	}
	if cfg.RequestTimeout != 0 {
		t.Errorf("expected request timeout 0, got %v", cfg.RequestTimeout)
	}
}

func TestLoadConfig_RequestTimeout(t *testing.T) {
	v := viper.New()
	v.SetConfigType("toml")
	configPath := filepath.Join(t.TempDir(), ".ai-mr-comment.toml")
	v.SetConfigFile(configPath)
	if err := os.WriteFile(configPath, []byte(`request_timeout = "45s"`), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := loadConfigWith(v, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RequestTimeout != 45*time.Second {
		t.Fatalf("expected 45s timeout, got %v", cfg.RequestTimeout)
	}
}

func TestLoadConfig_EnvVars(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-key-123")
	t.Setenv("AI_MR_COMMENT_PROVIDER", "ollama")

	v := viper.New()
	v.AutomaticEnv()
	v.SetEnvPrefix("AI_MR_COMMENT")
	_ = v.BindEnv("openai_api_key", "OPENAI_API_KEY")

	cfg, err := loadConfigWith(v, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.OpenAIAPIKey != "env-key-123" {
		t.Errorf("expected OpenAIAPIKey to be 'env-key-123', got '%s'", cfg.OpenAIAPIKey)
	}
	if cfg.Provider != Ollama {
		t.Errorf("expected Provider to be 'ollama', got '%s'", cfg.Provider)
	}
}

func TestLoadConfig_Precedence(t *testing.T) {
	// 1. Setup config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".ai-mr-comment.toml")
	content := `openai_api_key = "file-key"`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// 2. Setup Env Var
	t.Setenv("OPENAI_API_KEY", "env-key")

	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("toml")
	v.AutomaticEnv()
	_ = v.BindEnv("openai_api_key", "OPENAI_API_KEY")

	cfg, err := loadConfigWith(v, "")
	if err != nil {
		t.Fatal(err)
	}

	// Env should override file
	if cfg.OpenAIAPIKey != "env-key" {
		t.Errorf("expected env var to override config file. Got %s, want env-key", cfg.OpenAIAPIKey)
	}
}

func TestLoadConfig_Gemini(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "gemini-env-key")
	t.Setenv("AI_MR_COMMENT_PROVIDER", "gemini")

	v := viper.New()
	v.AutomaticEnv()
	v.SetEnvPrefix("AI_MR_COMMENT")
	_ = v.BindEnv("gemini_api_key", "GEMINI_API_KEY")

	cfg, err := loadConfigWith(v, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.GeminiAPIKey != "gemini-env-key" {
		t.Errorf("expected GeminiAPIKey to be 'gemini-env-key', got '%s'", cfg.GeminiAPIKey)
	}
	if cfg.Provider != Gemini {
		t.Errorf("expected Provider to be 'gemini', got '%s'", cfg.Provider)
	}
}

func TestLoadConfig_GitHubToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "gh-token-abc")

	v := viper.New()
	v.AutomaticEnv()
	_ = v.BindEnv("github_token", "GITHUB_TOKEN")

	cfg, err := loadConfigWith(v, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.GitHubToken != "gh-token-abc" {
		t.Errorf("expected GitHubToken 'gh-token-abc', got %q", cfg.GitHubToken)
	}
}

func TestLoadConfig_BaseURLs(t *testing.T) {
	t.Setenv("GITHUB_BASE_URL", "https://github.myco.com")
	t.Setenv("GITLAB_BASE_URL", "https://gitlab.myco.com")

	v := viper.New()
	v.AutomaticEnv()
	_ = v.BindEnv("github_base_url", "GITHUB_BASE_URL")
	_ = v.BindEnv("gitlab_base_url", "GITLAB_BASE_URL")

	cfg, err := loadConfigWith(v, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.GitHubBaseURL != "https://github.myco.com" {
		t.Errorf("expected GitHubBaseURL 'https://github.myco.com', got %q", cfg.GitHubBaseURL)
	}
	if cfg.GitLabBaseURL != "https://gitlab.myco.com" {
		t.Errorf("expected GitLabBaseURL 'https://gitlab.myco.com', got %q", cfg.GitLabBaseURL)
	}
}

func TestLoadConfig_MalformedTOML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".ai-mr-comment.toml")

	// Malformed content (missing quotes)
	content := `provider = anthropic`

	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("toml")

	_, err := loadConfigWith(v, "")
	if err == nil {
		t.Fatal("expected an error for malformed TOML, but got nil")
	}
	if !strings.Contains(err.Error(), "malformed config file") {
		t.Errorf("expected error to contain 'malformed config file', got '%v'", err)
	}
}

// newViperFromFile returns a Viper instance pre-configured to read from path.
// Used in tests to validate generated config files.
func newViperFromFile(path string) *viper.Viper {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("toml")
	return v
}

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, ".ai-mr-comment.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}
	return path
}

func TestLoadConfigWith_ProfileApplied(t *testing.T) {
	path := writeConfigFile(t, `
provider = "openai"
openai_model = "gpt-4.1-mini"

[profile.work]
provider = "anthropic"
anthropic_model = "claude-opus-4-6"
template = "technical"
`)
	v := newViperFromFile(path)
	cfg, err := loadConfigWith(v, "work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Provider != Anthropic {
		t.Errorf("expected provider anthropic, got %v", cfg.Provider)
	}
	if cfg.AnthropicModel != "claude-opus-4-6" {
		t.Errorf("expected anthropic_model claude-opus-4-6, got %v", cfg.AnthropicModel)
	}
	if cfg.Template != "technical" {
		t.Errorf("expected template technical, got %v", cfg.Template)
	}
}

func TestLoadConfigWith_ProfileNotFound(t *testing.T) {
	path := writeConfigFile(t, `provider = "openai"`)
	v := newViperFromFile(path)
	_, err := loadConfigWith(v, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing profile, got nil")
	}
	if !strings.Contains(err.Error(), `profile "nonexistent" not found`) {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestLoadConfigWith_EmptyProfileIsNoop(t *testing.T) {
	path := writeConfigFile(t, `provider = "gemini"`)
	v := newViperFromFile(path)
	cfg, err := loadConfigWith(v, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Provider != Gemini {
		t.Errorf("expected provider gemini, got %v", cfg.Provider)
	}
}

func TestLoadConfigWith_ProfilePartialOverride(t *testing.T) {
	path := writeConfigFile(t, `
provider = "openai"
openai_model = "gpt-4.1-mini"
anthropic_model = "claude-sonnet-4-6"

[profile.switch]
provider = "anthropic"
`)
	v := newViperFromFile(path)
	cfg, err := loadConfigWith(v, "switch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Provider != Anthropic {
		t.Errorf("expected provider anthropic, got %v", cfg.Provider)
	}
	// Non-overridden keys should keep their file/default values.
	if cfg.OpenAIModel != "gpt-4.1-mini" {
		t.Errorf("expected openai_model gpt-4.1-mini, got %v", cfg.OpenAIModel)
	}
	if cfg.AnthropicModel != "claude-sonnet-4-6" {
		t.Errorf("expected anthropic_model claude-sonnet-4-6, got %v", cfg.AnthropicModel)
	}
}

func TestProfileFlag_RootCmd(t *testing.T) {
	path := writeConfigFile(t, `
provider = "openai"
openai_api_key = "default-key"

[profile.test]
provider = "anthropic"
anthropic_api_key = "test-anthropic-key"
`)

	var capturedProvider ApiProvider
	mockFn := func(_ context.Context, cfg *Config, _ ApiProvider, _, _ string) (string, error) {
		capturedProvider = cfg.Provider
		return "ok", nil
	}

	t.Setenv("AI_MR_COMMENT_CONFIG", path) // not used by viper directly; we rely on the TOML path
	// We use loadConfigForProfile directly via the flag, so set HOME to the tmpDir
	// so viper finds our config file. We need the file at $HOME/.ai-mr-comment.toml.
	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, ".ai-mr-comment.toml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if err := os.WriteFile(destPath, content, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("HOME", tmpDir)

	cmd := newRootCmd(mockFn)
	cmd.SetArgs([]string{"--profile=test", "--file=testdata/simple.diff"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedProvider != Anthropic {
		t.Errorf("expected profile to set provider=anthropic, got %v", capturedProvider)
	}
}

func TestProfileFlag_ChangelogCmd(t *testing.T) {
	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, ".ai-mr-comment.toml")
	if err := os.WriteFile(destPath, []byte(`
provider = "openai"
openai_api_key = "default-key"

[profile.cl]
provider = "anthropic"
anthropic_api_key = "cl-anthropic-key"
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("HOME", tmpDir)

	var capturedProvider ApiProvider
	mockFn := func(_ context.Context, cfg *Config, _ ApiProvider, _, _ string) (string, error) {
		capturedProvider = cfg.Provider
		return "## Added\n- something", nil
	}

	cmd := newRootCmd(mockFn)
	cmd.SetArgs([]string{"changelog", "--profile=cl", "--file=testdata/simple.diff"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedProvider != Anthropic {
		t.Errorf("expected profile to set provider=anthropic, got %v", capturedProvider)
	}
}
