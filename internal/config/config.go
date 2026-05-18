package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// Provider identifies which AI backend to use.
type Provider string

const (
	OpenAI    Provider = "openai"
	Anthropic Provider = "anthropic"
	Ollama    Provider = "ollama"
	Gemini    Provider = "gemini"
	ClaudeCLI Provider = "claude-cli"
	GeminiCLI Provider = "gemini-cli"
	CodexCLI  Provider = "codex-cli"
)

// Config holds all runtime settings, populated from the TOML config file,
// environment variables, and CLI flags.
type Config struct {
	OpenAIAPIKey    string `mapstructure:"openai_api_key"`
	AnthropicAPIKey string `mapstructure:"anthropic_api_key"`
	GeminiAPIKey    string `mapstructure:"gemini_api_key"`
	GitHubToken     string `mapstructure:"github_token"`
	GitLabToken     string `mapstructure:"gitlab_token"`
	GitHubBaseURL   string `mapstructure:"github_base_url"`
	GitLabBaseURL   string `mapstructure:"gitlab_base_url"`

	OpenAIModel    string `mapstructure:"openai_model"`
	AnthropicModel string `mapstructure:"anthropic_model"`
	OllamaModel    string `mapstructure:"ollama_model"`
	GeminiModel    string `mapstructure:"gemini_model"`
	ClaudeCLIModel string `mapstructure:"claude_cli_model"`
	ClaudeCLIPath  string `mapstructure:"claude_cli_path"`
	GeminiCLIModel string `mapstructure:"gemini_cli_model"`
	GeminiCLIPath  string `mapstructure:"gemini_cli_path"`
	CodexCLIModel  string `mapstructure:"codex_cli_model"`
	CodexCLIPath   string `mapstructure:"codex_cli_path"`

	OpenAIEndpoint    string        `mapstructure:"openai_endpoint"`
	AnthropicEndpoint string        `mapstructure:"anthropic_endpoint"`
	OllamaEndpoint    string        `mapstructure:"ollama_endpoint"`
	Provider          Provider      `mapstructure:"provider"`
	Template          string        `mapstructure:"template"`
	RequestTimeout    time.Duration `mapstructure:"request_timeout"`

	// DebugWriter is the output destination for verbose debug messages.
	// Nil when verbose mode is disabled. Set by the CLI after config load; never read from TOML.
	DebugWriter io.Writer

	// ConfigFile is the path of the TOML config file that was loaded, or "" if none was found.
	// Set by LoadWith; never read from TOML.
	ConfigFile string
}

// Load reads configuration from ~/.ai-mr-comment.toml (or the current
// directory) and standard environment variables such as OPENAI_API_KEY.
func Load() (*Config, error) {
	return LoadForProfile("")
}

// LoadForProfile reads configuration like Load, then overlays the named profile
// section (e.g. [profile.work]) if profile is non-empty.
func LoadForProfile(profile string) (*Config, error) {
	v := viper.New()
	v.SetConfigName(".ai-mr-comment")
	v.SetConfigType("toml")
	v.AddConfigPath(".")
	v.AddConfigPath("$HOME")

	v.AutomaticEnv()
	v.SetEnvPrefix("AI_MR_COMMENT")

	// Bind the conventional bare env vars in addition to the prefixed ones.
	_ = v.BindEnv("openai_api_key", "OPENAI_API_KEY")
	_ = v.BindEnv("anthropic_api_key", "ANTHROPIC_API_KEY")
	_ = v.BindEnv("gemini_api_key", "GEMINI_API_KEY")
	_ = v.BindEnv("github_token", "GITHUB_TOKEN")
	_ = v.BindEnv("gitlab_token", "GITLAB_TOKEN")
	_ = v.BindEnv("github_base_url", "GITHUB_BASE_URL")
	_ = v.BindEnv("gitlab_base_url", "GITLAB_BASE_URL")

	return LoadWith(v, profile)
}

func ListProfiles() []string {
	v := viper.New()
	v.SetConfigName(".ai-mr-comment")
	v.SetConfigType("toml")
	v.AddConfigPath(".")
	v.AddConfigPath("$HOME")
	if err := v.ReadInConfig(); err != nil {
		return nil
	}
	sub := v.Sub("profile")
	if sub == nil {
		return nil
	}
	settings := sub.AllSettings()
	profiles := make([]string, 0, len(settings))
	for name := range settings {
		profiles = append(profiles, name)
	}
	sort.Strings(profiles)
	return profiles
}

// ApplyProfile overlays values from [profile.<name>] in v onto the base config.
// Returns an error if profile is non-empty but not defined in the config file.
func ApplyProfile(v *viper.Viper, profile string) error {
	if profile == "" {
		return nil
	}
	sub := v.Sub("profile." + profile)
	if sub == nil {
		return fmt.Errorf("profile %q not found in config", profile)
	}
	for key, val := range sub.AllSettings() {
		if hasEnvOverride(key) {
			continue
		}
		v.Set(key, val)
	}
	return nil
}

func hasEnvOverride(key string) bool {
	envKey := "AI_MR_COMMENT_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
	if _, ok := os.LookupEnv(envKey); ok {
		return true
	}
	for _, bare := range bareEnvKeys(key) {
		if _, ok := os.LookupEnv(bare); ok {
			return true
		}
	}
	return false
}

func bareEnvKeys(key string) []string {
	switch key {
	case "openai_api_key":
		return []string{"OPENAI_API_KEY"}
	case "anthropic_api_key":
		return []string{"ANTHROPIC_API_KEY"}
	case "gemini_api_key":
		return []string{"GEMINI_API_KEY"}
	case "github_token":
		return []string{"GITHUB_TOKEN"}
	case "gitlab_token":
		return []string{"GITLAB_TOKEN"}
	case "github_base_url":
		return []string{"GITHUB_BASE_URL"}
	case "gitlab_base_url":
		return []string{"GITLAB_BASE_URL"}
	default:
		return nil
	}
}

// LoadWith applies defaults, reads the config file (if present), overlays the
// named profile (if any), and unmarshals the result into a Config.
// It is split from LoadForProfile to allow tests to inject a pre-configured Viper instance.
func LoadWith(v *viper.Viper, profile string) (*Config, error) {
	v.SetDefault("provider", Anthropic)
	v.SetDefault("openai_model", "gpt-5.5")
	v.SetDefault("openai_endpoint", "https://api.openai.com/v1/")
	v.SetDefault("anthropic_model", "claude-sonnet-4-6")
	v.SetDefault("anthropic_endpoint", "https://api.anthropic.com/")
	v.SetDefault("ollama_model", "llama3.2")
	v.SetDefault("ollama_endpoint", "http://localhost:11434/api/generate")
	v.SetDefault("gemini_model", "gemini-2.5-flash")
	v.SetDefault("claude_cli_model", "claude-sonnet-4-6")
	v.SetDefault("claude_cli_path", "")
	v.SetDefault("gemini_cli_model", "gemini-2.5-flash")
	v.SetDefault("gemini_cli_path", "")
	v.SetDefault("codex_cli_model", "")
	v.SetDefault("codex_cli_path", "")
	v.SetDefault("template", "default")
	v.SetDefault("request_timeout", "0s")

	if err := v.ReadInConfig(); err != nil {
		var configParseError viper.ConfigParseError
		if errors.As(err, &configParseError) {
			return nil, fmt.Errorf("malformed config file: %w", err)
		}

		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return nil, err
		}
	}

	if err := ApplyProfile(v, profile); err != nil {
		return nil, err
	}

	cfg := &Config{}
	// Strip the "profile" subtree before unmarshalling so that UnmarshalExact
	// does not reject it as an unknown key.
	if err := Unmarshal(v, cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	cfg.ConfigFile = v.ConfigFileUsed()

	return cfg, nil
}

// Unmarshal decodes Viper's settings into cfg, excluding the "profile"
// subtree which is not part of Config but is valid in the TOML file.
func Unmarshal(v *viper.Viper, cfg *Config) error {
	settings := v.AllSettings()
	delete(settings, "profile")
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		ErrorUnused:      true,
		WeaklyTypedInput: false,
		Result:           cfg,
		TagName:          "mapstructure",
		DecodeHook:       mapstructure.ComposeDecodeHookFunc(mapstructure.StringToTimeDurationHookFunc()),
	})
	if err != nil {
		return err
	}
	return decoder.Decode(settings)
}
