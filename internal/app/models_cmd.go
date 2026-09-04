package app

import (
	"fmt"

	"github.com/spf13/cobra"
)

// providerModels lists known models for each provider, used by the `models` subcommand.
var providerModels = map[ApiProvider][]string{
	OpenAI: {
		"gpt-5.5",
		"gpt-5.5-pro",
		"gpt-5.4",
		"gpt-5.4-pro",
		"gpt-5.4-mini",
		"gpt-5.4-nano",
		"gpt-5.3-codex",
		"gpt-5.2",
		"gpt-4.1",
		"gpt-4.1-mini",
		"gpt-4.1-nano",
		"o3",
		"o3-mini",
		"gpt-4o",
		"gpt-4o-mini",
	},
	Anthropic: {
		"claude-opus-4-7",
		"claude-opus-4-6",
		"claude-sonnet-4-6",
		"claude-haiku-4-5-20251001",
		"claude-opus-4-5-20251101",
		"claude-sonnet-4-5-20250929",
		"claude-sonnet-4-20250514",
		"claude-3-7-sonnet-20250219",
		"claude-3-haiku-20240307",
	},
	Gemini: {
		"gemini-3.1-pro-preview",
		"gemini-3.1-pro-preview-customtools",
		"gemini-3-flash-preview",
		"gemini-3.1-flash-lite",
		"gemini-3.1-flash-lite-preview",
		"gemini-2.5-pro",
		"gemini-2.5-flash",
		"gemini-2.5-flash-lite",
		"gemini-2.0-flash",
	},
	Ollama: {
		"llama3",
		"llama3.1",
		"llama3.2",
		"mistral",
		"codellama",
		"phi3",
	},
	ClaudeCLI: {
		"claude-opus-4-7",
		"claude-opus-4-6",
		"claude-sonnet-4-6",
		"claude-haiku-4-5-20251001",
	},
	GeminiCLI: {
		"gemini-3.1-pro-preview",
		"gemini-3-flash-preview",
		"gemini-3.1-flash-lite",
		"gemini-2.5-pro",
		"gemini-2.5-flash",
		"gemini-2.5-flash-lite",
	},
	CodexCLI: {
		// Model names depend on what is configured in the local codex CLI.
		// Leave empty to use codex default, or set codex_cli_model in config.
	},
}

// newModelsCmd returns the models subcommand, which lists known models for the active provider.
func newModelsCmd() *cobra.Command {
	var provider string
	var profileName string

	cmd := &cobra.Command{
		Use:   "models",
		Short: "List available models for a provider",
		Long:  `Prints the known model names for the given provider. Use --provider to select a provider (defaults to the configured one).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := ApiProvider(provider)
			if !cmd.Flags().Changed("provider") {
				cfg, err := loadConfigForProfile(profileName)
				if err != nil {
					return err
				}
				p = cfg.Provider
			}
			models, ok := providerModels[p]
			if !ok {
				return withExitCode(4, fmt.Errorf("unknown provider %q: choose from openai, anthropic, gemini, ollama, claude-cli, gemini-cli, codex-cli", p))
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Models for provider %s:\n\n", p)
			if len(models) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  (no fixed model list — configured by the local CLI tool)")
			}
			for _, m := range models {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", m)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Use --model <name> to select a model for a run.\n")
			return nil
		},
	}

	cmd.Flags().StringVar(&provider, "provider", "", "Provider to list models for (openai, anthropic, gemini, ollama, claude-cli, gemini-cli, codex-cli)")
	cmd.Flags().StringVar(&profileName, "profile", "", "Named config profile to activate")
	_ = cmd.RegisterFlagCompletionFunc("provider", completeValues(providerNames))
	_ = cmd.RegisterFlagCompletionFunc("profile", completeProfiles)
	return cmd
}
