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
)

type Config struct {
	OpenAIKey    string `mapstructure:"openai_api_key"`
	AnthropicKey string `mapstructure:"anthropic_api_key"`

	OpenAIModel    string `mapstructure:"openai_model"`
	AnthropicModel string `mapstructure:"anthropic_model"`
	OllamaModel    string `mapstructure:"ollama_model"`

	OpenAIEndpoint    string      `mapstructure:"openai_endpoint"`
	AnthropicEndpoint string      `mapstructure:"anthropic_endpoint"`
	OllamaEndpoint    string      `mapstructure:"ollama_endpoint"`
	Provider          ApiProvider `mapstructure:"provider"`
}

func loadConfig() (*Config, error) {
	v := viper.New()
	v.SetConfigName(".ai-mr-comment.toml")
	v.SetConfigType("toml")
	v.AddConfigPath("$HOME")
	v.AutomaticEnv()
	v.SetEnvPrefix("AI_MR_COMMENT")

	return loadConfigWith(v)
}

func loadConfigWith(v *viper.Viper) (*Config, error) {
	cfg := &Config{
		Provider:          OpenAI,
		OpenAIModel:       "gpt-4o-mini",
		OpenAIEndpoint:    "https://api.openai.com/v1/chat/completions",
		AnthropicModel:    "claude-3-7-sonnet-20250219",
		AnthropicEndpoint: "https://api.anthropic.com/v1/messages",
	}

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return nil, err
		}
	} else {
		if err := v.UnmarshalExact(cfg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}

	return cfg, nil
}
