package app

import (
	internalconfig "github.com/pbsladek/ai-mr-comment/internal/config"
	"github.com/spf13/viper"
)

// ApiProvider identifies which AI backend to use.
type ApiProvider = internalconfig.Provider

const (
	OpenAI    = internalconfig.OpenAI
	Anthropic = internalconfig.Anthropic
	Ollama    = internalconfig.Ollama
	Gemini    = internalconfig.Gemini
	ClaudeCLI = internalconfig.ClaudeCLI
	GeminiCLI = internalconfig.GeminiCLI
	CodexCLI  = internalconfig.CodexCLI
)

// Config holds all runtime settings, populated from the TOML config file,
// environment variables, and CLI flags.
type Config = internalconfig.Config

// loadConfig reads configuration from ~/.ai-mr-comment.toml (or the current
// directory) and standard environment variables such as OPENAI_API_KEY.
func loadConfig() (*Config, error) {
	return internalconfig.Load()
}

// loadConfigForProfile reads configuration like loadConfig, then overlays the
// named profile section (e.g. [profile.work]) if profile is non-empty.
func loadConfigForProfile(profile string) (*Config, error) {
	return internalconfig.LoadForProfile(profile)
}

func listConfigProfiles() []string {
	return internalconfig.ListProfiles()
}

// loadConfigWith applies defaults, reads the config file (if present), overlays
// the named profile (if any), and unmarshals the result into a Config.
// It is split from loadConfigForProfile to allow tests to inject a pre-configured Viper instance.
func loadConfigWith(v *viper.Viper, profile string) (*Config, error) {
	return internalconfig.LoadWith(v, profile)
}
