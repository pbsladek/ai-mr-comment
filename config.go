package main

import (
	_ "embed"
	"fmt"

	"github.com/spf13/viper"
)

type ApiProvider string

const (
	OpenAI ApiProvider = "openai"
	Claude ApiProvider = "claude"
)

type Config struct {
	OpenAIKey      string      `mapstructure:"openai_api_key"`
	ClaudeKey      string      `mapstructure:"claude_api_key"`
	OpenAIModel    string      `mapstructure:"openai_model"`
	ClaudeModel    string      `mapstructure:"claude_model"`
	OpenAIEndpoint string      `mapstructure:"openai_endpoint"`
	ClaudeEndpoint string      `mapstructure:"claude_endpoint"`
	Provider       ApiProvider `mapstructure:"provider"`
}

func loadConfig() (*Config, error) {
	cfg := &Config{
		Provider:       OpenAI,
		OpenAIModel:    "gpt-4o-mini",
		OpenAIEndpoint: "https://api.openai.com/v1/chat/completions",
		ClaudeModel:    "claude-3-7-sonnet-20250219",
		ClaudeEndpoint: "https://api.anthropic.com/v1/messages",
	}
	viper.SetConfigName(".ai-mr-comment.toml")
	viper.SetConfigType("toml")
	viper.AddConfigPath("$HOME")
	if err := viper.ReadInConfig(); err == nil {
		viper.AutomaticEnv()
		viper.SetEnvPrefix("AI_MR_COMMENT")
		if err := viper.UnmarshalExact(cfg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}
	return cfg, nil
}
