package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestLoadWithDefaultsWhenMissingFile(t *testing.T) {
	v := viper.New()
	v.SetConfigName(".ai-mr-comment")
	v.SetConfigType("toml")
	v.AddConfigPath(filepath.Join(t.TempDir(), "missing"))

	cfg, err := LoadWith(v, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Provider != DefaultProvider {
		t.Errorf("expected provider %q, got %q", DefaultProvider, cfg.Provider)
	}
	if cfg.OpenAIModel != DefaultOpenAIModel {
		t.Errorf("expected OpenAI default model %q, got %q", DefaultOpenAIModel, cfg.OpenAIModel)
	}
	if cfg.AnthropicModel != DefaultAnthropicModel {
		t.Errorf("expected Anthropic default model %q, got %q", DefaultAnthropicModel, cfg.AnthropicModel)
	}
	if cfg.GeminiModel != DefaultGeminiModel {
		t.Errorf("expected Gemini default model %q, got %q", DefaultGeminiModel, cfg.GeminiModel)
	}
	if cfg.OllamaModel != DefaultOllamaModel {
		t.Errorf("expected Ollama default model %q, got %q", DefaultOllamaModel, cfg.OllamaModel)
	}
	if cfg.ClaudeCLIModel != DefaultClaudeCLIModel {
		t.Errorf("expected Claude CLI default model %q, got %q", DefaultClaudeCLIModel, cfg.ClaudeCLIModel)
	}
	if cfg.GeminiCLIModel != DefaultGeminiCLIModel {
		t.Errorf("expected Gemini CLI default model %q, got %q", DefaultGeminiCLIModel, cfg.GeminiCLIModel)
	}
	if cfg.CodexCLIModel != DefaultCodexCLIModel {
		t.Errorf("expected Codex CLI default model %q, got %q", DefaultCodexCLIModel, cfg.CodexCLIModel)
	}
	if cfg.Template != DefaultTemplate {
		t.Errorf("expected template default %q, got %q", DefaultTemplate, cfg.Template)
	}
	if cfg.RequestTimeout != 0 {
		t.Errorf("expected zero request timeout, got %v", cfg.RequestTimeout)
	}
}

func TestLoadWithProfileOverlay(t *testing.T) {
	path := writeConfig(t, `
provider = "openai"
openai_model = "gpt-4.1-mini"
request_timeout = "30s"

[profile.review]
provider = "anthropic"
anthropic_model = "claude-opus-4-6"
template = "technical"
`)

	cfg, err := LoadWith(viperFromFile(path), "review")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Provider != Anthropic {
		t.Errorf("expected profile provider anthropic, got %q", cfg.Provider)
	}
	if cfg.OpenAIModel != "gpt-4.1-mini" {
		t.Errorf("expected non-overridden openai_model to remain, got %q", cfg.OpenAIModel)
	}
	if cfg.AnthropicModel != "claude-opus-4-6" {
		t.Errorf("expected profile anthropic_model, got %q", cfg.AnthropicModel)
	}
	if cfg.Template != "technical" {
		t.Errorf("expected profile template technical, got %q", cfg.Template)
	}
	if cfg.RequestTimeout != 30*time.Second {
		t.Errorf("expected request timeout 30s, got %v", cfg.RequestTimeout)
	}
}

func TestLoadWithProfileDoesNotOverrideEnv(t *testing.T) {
	t.Setenv("AI_MR_COMMENT_PROVIDER", "gemini")
	t.Setenv("OPENAI_API_KEY", "env-openai-key")

	path := writeConfig(t, `
provider = "openai"
openai_api_key = "file-key"

[profile.review]
provider = "anthropic"
openai_api_key = "profile-key"
anthropic_model = "claude-opus-4-6"
`)

	v := viperFromFile(path)
	v.AutomaticEnv()
	v.SetEnvPrefix("AI_MR_COMMENT")
	_ = v.BindEnv("openai_api_key", "OPENAI_API_KEY")

	cfg, err := LoadWith(v, "review")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Provider != Gemini {
		t.Fatalf("expected env provider to win over profile, got %q", cfg.Provider)
	}
	if cfg.OpenAIAPIKey != "env-openai-key" {
		t.Fatalf("expected env API key to win over profile, got %q", cfg.OpenAIAPIKey)
	}
	if cfg.AnthropicModel != "claude-opus-4-6" {
		t.Fatalf("expected non-env profile value to apply, got %q", cfg.AnthropicModel)
	}
}

func TestLoadWithMissingProfile(t *testing.T) {
	path := writeConfig(t, `provider = "openai"`)

	_, err := LoadWith(viperFromFile(path), "missing")
	if err == nil {
		t.Fatal("expected missing profile error")
	}
	if !strings.Contains(err.Error(), `profile "missing" not found`) {
		t.Fatalf("expected missing profile message, got %v", err)
	}
}

func TestLoadWithMalformedConfig(t *testing.T) {
	path := writeConfig(t, `provider = anthropic`)

	_, err := LoadWith(viperFromFile(path), "")
	if err == nil {
		t.Fatal("expected malformed config error")
	}
	if !strings.Contains(err.Error(), "malformed config file") {
		t.Fatalf("expected malformed config message, got %v", err)
	}
}

func TestLoadAndLoadForProfileEntryPoints(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".ai-mr-comment.toml"), []byte(`
provider = "openai"
openai_model = "gpt-file"

[profile.work]
provider = "gemini"
gemini_model = "gemini-file"
`), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}
	t.Chdir(dir)
	t.Setenv("HOME", filepath.Join(dir, "missing-home"))
	t.Setenv("OPENAI_API_KEY", "env-openai")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Provider != OpenAI || cfg.OpenAIAPIKey != "env-openai" || cfg.OpenAIModel != "gpt-file" {
		t.Fatalf("Load config = %+v", cfg)
	}

	cfg, err = LoadForProfile("work")
	if err != nil {
		t.Fatalf("LoadForProfile failed: %v", err)
	}
	if cfg.Provider != Gemini || cfg.GeminiModel != "gemini-file" {
		t.Fatalf("profile config = %+v", cfg)
	}
}

func TestListProfiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".ai-mr-comment.toml"), []byte(`
[profile.zed]
provider = "openai"

[profile.alpha]
provider = "anthropic"
`), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}
	t.Chdir(dir)

	got := ListProfiles()
	want := []string{"alpha", "zed"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("profiles mismatch:\ngot  %v\nwant %v", got, want)
	}
}

func TestListProfilesNoConfigAndEnvOverrideHelpers(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	if got := ListProfiles(); got != nil {
		t.Fatalf("expected nil profiles without config, got %v", got)
	}

	for _, tc := range []struct {
		key  string
		want string
	}{
		{"anthropic_api_key", "ANTHROPIC_API_KEY"},
		{"gemini_api_key", "GEMINI_API_KEY"},
		{"github_token", "GITHUB_TOKEN"},
		{"gitlab_token", "GITLAB_TOKEN"},
		{"github_base_url", "GITHUB_BASE_URL"},
		{"gitlab_base_url", "GITLAB_BASE_URL"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			got := bareEnvKeys(tc.key)
			if len(got) != 1 || got[0] != tc.want {
				t.Fatalf("bareEnvKeys(%q) = %v", tc.key, got)
			}
		})
	}
	if got := bareEnvKeys("unknown"); got != nil {
		t.Fatalf("unknown bare env keys = %v", got)
	}

	t.Setenv("AI_MR_COMMENT_OPENAI_MODEL", "gpt-env")
	if !hasEnvOverride("openai_model") {
		t.Fatal("expected prefixed env override")
	}
	t.Setenv("ANTHROPIC_API_KEY", "anthropic")
	if !hasEnvOverride("anthropic_api_key") {
		t.Fatal("expected bare env override")
	}
}

func viperFromFile(path string) *viper.Viper {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("toml")
	return v
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), ".ai-mr-comment.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}
	return path
}
