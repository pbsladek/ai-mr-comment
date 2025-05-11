package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_WithValidTOML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".ai-mr-comment.toml")

	content := `
provider = "anthropic"
openai_model = "gpt-4"
anthropic_model = "claude-3-opus"
`

	require.NoError(t, os.WriteFile(configPath, []byte(content), 0644))

	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("toml")

	cfg, err := loadConfigWith(v)
	require.NoError(t, err)
	require.Equal(t, Anthropic, cfg.Provider)
	require.Equal(t, "gpt-4", cfg.OpenAIModel)
	require.Equal(t, "claude-3-opus", cfg.AnthropicModel)
}

func TestLoadConfig_DefaultsWhenMissingFile(t *testing.T) {
	v := viper.New()
	v.SetConfigName(".ai-mr-comment.toml")
	v.SetConfigType("toml")
	v.AddConfigPath("/nonexistent") // a directory that doesn't exist

	cfg, err := loadConfigWith(v)
	require.NoError(t, err)
	require.Equal(t, OpenAI, cfg.Provider)
	require.Equal(t, "gpt-4o-mini", cfg.OpenAIModel)
	require.Equal(t, "claude-3-7-sonnet-20250219", cfg.AnthropicModel)
}
