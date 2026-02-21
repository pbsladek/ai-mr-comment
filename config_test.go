package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	cfg, err := loadConfigWith(v)
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

	cfg, err := loadConfigWith(v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Provider != OpenAI {
		t.Errorf("expected provider OpenAI, got %v", cfg.Provider)
	}
	if cfg.OpenAIModel != "gpt-4.1-mini" {
		t.Errorf("expected OpenAI model gpt-4.1-mini, got %v", cfg.OpenAIModel)
	}
	if cfg.AnthropicModel != "claude-sonnet-4-6" {
		t.Errorf("expected Anthropic model claude-sonnet-4-6, got %v", cfg.AnthropicModel)
	}
}

func TestLoadConfig_EnvVars(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-key-123")
	t.Setenv("AI_MR_COMMENT_PROVIDER", "ollama")

	v := viper.New()
	v.AutomaticEnv()
	v.SetEnvPrefix("AI_MR_COMMENT")
	_ = v.BindEnv("openai_api_key", "OPENAI_API_KEY")

	cfg, err := loadConfigWith(v)
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

	cfg, err := loadConfigWith(v)
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

	cfg, err := loadConfigWith(v)
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

	cfg, err := loadConfigWith(v)
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

	cfg, err := loadConfigWith(v)
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

	_, err := loadConfigWith(v)
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
