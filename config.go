package main

import (
	_ "embed"
	"errors"
	"fmt"
	"io"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// ApiProvider identifies which AI backend to use.
type ApiProvider string

const (
	OpenAI    ApiProvider = "openai"
	Anthropic ApiProvider = "anthropic"
	Ollama    ApiProvider = "ollama"
	Gemini    ApiProvider = "gemini"
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

	OpenAIEndpoint    string      `mapstructure:"openai_endpoint"`
	AnthropicEndpoint string      `mapstructure:"anthropic_endpoint"`
	OllamaEndpoint    string      `mapstructure:"ollama_endpoint"`
	Provider          ApiProvider `mapstructure:"provider"`
	Template          string      `mapstructure:"template"`

	// DebugWriter is the output destination for verbose debug messages.
	// Nil when verbose mode is disabled. Set by the CLI after config load; never read from TOML.
	DebugWriter io.Writer

	// ConfigFile is the path of the TOML config file that was loaded, or "" if none was found.
	// Set by loadConfigWith; never read from TOML.
	ConfigFile string
}

// loadConfig reads configuration from ~/.ai-mr-comment.toml (or the current
// directory) and standard environment variables such as OPENAI_API_KEY.
func loadConfig() (*Config, error) {
	return loadConfigForProfile("")
}

// loadConfigForProfile reads configuration like loadConfig, then overlays the
// named profile section (e.g. [profile.work]) if profile is non-empty.
func loadConfigForProfile(profile string) (*Config, error) {
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

	return loadConfigWith(v, profile)
}

// applyProfile overlays values from [profile.<name>] in v onto the base config.
// Returns an error if profile is non-empty but not defined in the config file.
func applyProfile(v *viper.Viper, profile string) error {
	if profile == "" {
		return nil
	}
	sub := v.Sub("profile." + profile)
	if sub == nil {
		return fmt.Errorf("profile %q not found in config", profile)
	}
	for key, val := range sub.AllSettings() {
		v.Set(key, val)
	}
	return nil
}

// loadConfigWith applies defaults, reads the config file (if present), overlays
// the named profile (if any), and unmarshals the result into a Config.
// It is split from loadConfigForProfile to allow tests to inject a pre-configured Viper instance.
func loadConfigWith(v *viper.Viper, profile string) (*Config, error) {
	v.SetDefault("provider", Anthropic)
	v.SetDefault("openai_model", "gpt-4.1-mini")
	v.SetDefault("openai_endpoint", "https://api.openai.com/v1/")
	v.SetDefault("anthropic_model", "claude-sonnet-4-6")
	v.SetDefault("anthropic_endpoint", "https://api.anthropic.com/")
	v.SetDefault("ollama_model", "llama3")
	v.SetDefault("ollama_endpoint", "http://localhost:11434/api/generate")
	v.SetDefault("gemini_model", "gemini-2.5-flash")
	v.SetDefault("template", "default")

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

	if err := applyProfile(v, profile); err != nil {
		return nil, err
	}

	cfg := &Config{}
	// Strip the "profile" subtree before unmarshalling so that UnmarshalExact
	// does not reject it as an unknown key.
	if err := unmarshalConfig(v, cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	cfg.ConfigFile = v.ConfigFileUsed()

	return cfg, nil
}

// unmarshalConfig decodes Viper's settings into cfg, excluding the "profile"
// subtree which is not part of Config but is valid in the TOML file.
func unmarshalConfig(v *viper.Viper, cfg *Config) error {
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
