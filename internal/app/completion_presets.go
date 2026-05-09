package app

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var presetNames = []string{"ci", "local-fast", "security", "release-notes"}
var providerNames = []string{"openai", "anthropic", "gemini", "ollama", "claude-cli", "gemini-cli", "codex-cli"}
var templateNames = []string{"default", "conventional", "technical", "user-focused", "emoji", "sassy", "monday", "jira", "commit", "commit-emoji", "commit-conventional", "chaos", "haiku", "roast", "intern", "shakespeare", "manager", "yoda", "excuse"}

func completeValues(values []string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		matches := make([]string, 0, len(values))
		for _, value := range values {
			if strings.HasPrefix(value, toComplete) {
				matches = append(matches, value)
			}
		}
		return matches, cobra.ShellCompDirectiveNoFileComp
	}
}

func completeProfiles(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	profiles := listConfigProfiles()
	matches := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		if strings.HasPrefix(profile, toComplete) {
			matches = append(matches, profile)
		}
	}
	return matches, cobra.ShellCompDirectiveNoFileComp
}

func applyRootPreset(cmd *cobra.Command, name string, cfg *Config, format *string, exitCodeFlag, plain, generateTitle *bool) (promptSuffix string, err error) {
	switch name {
	case "":
		return "", nil
	case "ci":
		if !cmd.Flags().Changed("format") {
			*format = "json"
		}
		if !cmd.Flags().Changed("exit-code") {
			*exitCodeFlag = true
		}
		if !cmd.Flags().Changed("template") {
			cfg.Template = "technical"
		}
	case "local-fast":
		if !cmd.Flags().Changed("provider") {
			cfg.Provider = Ollama
		}
		if !cmd.Flags().Changed("model") {
			cfg.OllamaModel = "llama3.2"
		}
		if !cmd.Flags().Changed("plain") && !cmd.Flags().Changed("no-decorate") {
			*plain = true
		}
	case "security":
		if !cmd.Flags().Changed("template") {
			cfg.Template = "technical"
		}
		promptSuffix = "\n\nFocus especially on security vulnerabilities, unsafe defaults, credential exposure, injection risks, authorization bugs, and data handling issues."
	case "release-notes":
		if !cmd.Flags().Changed("template") {
			cfg.Template = "user-focused"
		}
		if !cmd.Flags().Changed("title") {
			*generateTitle = true
		}
	default:
		return "", fmt.Errorf("unknown preset %q: choose from %s", name, strings.Join(presetNames, ", "))
	}
	return promptSuffix, nil
}

func applyChangelogPreset(cmd *cobra.Command, name string, cfg *Config, format *string) (promptSuffix string, err error) {
	switch name {
	case "":
		return "", nil
	case "ci":
		if !cmd.Flags().Changed("format") {
			*format = "json"
		}
	case "local-fast":
		if !cmd.Flags().Changed("provider") {
			cfg.Provider = Ollama
		}
		if !cmd.Flags().Changed("model") {
			cfg.OllamaModel = "llama3.2"
		}
	case "security":
		promptSuffix = "\n\nCall out security-relevant changes, mitigations, and user-visible risk reductions clearly."
	case "release-notes":
		// The changelog command is already release-note oriented.
	default:
		return "", fmt.Errorf("unknown preset %q: choose from %s", name, strings.Join(presetNames, ", "))
	}
	return promptSuffix, nil
}

func isSupportedProvider(provider ApiProvider) bool {
	switch provider {
	case OpenAI, Anthropic, Gemini, Ollama, ClaudeCLI, GeminiCLI, CodexCLI:
		return true
	default:
		return false
	}
}
