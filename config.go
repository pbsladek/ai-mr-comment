package main

import (
	_ "embed"
	"errors"
	"fmt"

	"github.com/spf13/viper"
)

type ApiProvider string

const (
	OpenAI    ApiProvider = "openai"
	Anthropic ApiProvider = "anthropic"
	Ollama    ApiProvider = "ollama"
	Gemini    ApiProvider = "gemini"
)

type Config struct {
	OpenAIAPIKey    string `mapstructure:"openai_api_key"`
	AnthropicAPIKey string `mapstructure:"anthropic_api_key"`
	GeminiAPIKey    string `mapstructure:"gemini_api_key"`

	OpenAIModel    string `mapstructure:"openai_model"`
	AnthropicModel string `mapstructure:"anthropic_model"`
	OllamaModel    string `mapstructure:"ollama_model"`
	GeminiModel    string `mapstructure:"gemini_model"`

	OpenAIEndpoint    string      `mapstructure:"openai_endpoint"`
	AnthropicEndpoint string      `mapstructure:"anthropic_endpoint"`
	OllamaEndpoint    string      `mapstructure:"ollama_endpoint"`
	Provider          ApiProvider `mapstructure:"provider"`
}

func loadConfig() (*Config, error) {
	v := viper.New()
	v.SetConfigName(".ai-mr-comment")
	v.SetConfigType("toml")
	v.AddConfigPath(".")
	v.AddConfigPath("$HOME")

	v.AutomaticEnv()
	v.SetEnvPrefix("AI_MR_COMMENT")

	// Bind standard environment variables
	_ = v.BindEnv("openai_api_key", "OPENAI_API_KEY")
	_ = v.BindEnv("anthropic_api_key", "ANTHROPIC_API_KEY")
	_ = v.BindEnv("gemini_api_key", "GEMINI_API_KEY")

	return loadConfigWith(v)
}

func loadConfigWith(v *viper.Viper) (*Config, error) {
	v.SetDefault("provider", OpenAI)
	v.SetDefault("openai_model", "gpt-4o-mini")
	v.SetDefault("openai_endpoint", "https://api.openai.com/v1/chat/completions")
	v.SetDefault("anthropic_model", "claude-3-7-sonnet-20250219")
	v.SetDefault("anthropic_endpoint", "https://api.anthropic.com/v1/messages")
	v.SetDefault("ollama_model", "llama3")
	v.SetDefault("ollama_endpoint", "http://localhost:11434/api/generate")
	v.SetDefault("gemini_model", "gemini-1.5-flash")

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return nil, err
		}
	}

	cfg := &Config{}
	if err := v.UnmarshalExact(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return cfg, nil
}
