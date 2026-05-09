package app

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

var presetNames = []string{"ci", "local-fast", "security", "release-notes"}

type providerCallFunc func(context.Context, *Config, string, string) (string, error)
type providerStreamFunc func(context.Context, *Config, string, string, io.Writer) (string, error)
type providerModelFunc func(*Config) string

type providerMetadata struct {
	Name         string
	Provider     ApiProvider
	DefaultModel string
	RequiresKey  bool
	Streaming    bool
	ModelName    providerModelFunc
	Call         providerCallFunc
	Stream       providerStreamFunc
}

var providerRegistry = []providerMetadata{
	{
		Name:         "openai",
		Provider:     OpenAI,
		DefaultModel: "gpt-5.5",
		RequiresKey:  true,
		Streaming:    true,
		ModelName:    func(cfg *Config) string { return cfg.OpenAIModel },
		Call:         callOpenAIProvider,
		Stream:       streamOpenAIProvider,
	},
	{
		Name:         "anthropic",
		Provider:     Anthropic,
		DefaultModel: "claude-sonnet-4-6",
		RequiresKey:  true,
		Streaming:    true,
		ModelName:    func(cfg *Config) string { return cfg.AnthropicModel },
		Call:         callAnthropicProvider,
		Stream:       streamAnthropicProvider,
	},
	{
		Name:         "gemini",
		Provider:     Gemini,
		DefaultModel: "gemini-2.5-pro",
		RequiresKey:  true,
		Streaming:    true,
		ModelName:    func(cfg *Config) string { return cfg.GeminiModel },
		Call:         callGemini,
		Stream:       streamGemini,
	},
	{
		Name:         "ollama",
		Provider:     Ollama,
		DefaultModel: "llama3.2",
		Streaming:    true,
		ModelName:    func(cfg *Config) string { return cfg.OllamaModel },
		Call:         callOllama,
		Stream:       streamOllama,
	},
	{
		Name:         "claude-cli",
		Provider:     ClaudeCLI,
		DefaultModel: "claude-sonnet-4-6",
		Streaming:    true,
		ModelName:    func(cfg *Config) string { return cfg.ClaudeCLIModel },
		Call:         callClaudeCLI,
		Stream:       streamClaudeCLI,
	},
	{
		Name:         "gemini-cli",
		Provider:     GeminiCLI,
		DefaultModel: "gemini-2.5-flash",
		Streaming:    true,
		ModelName:    func(cfg *Config) string { return cfg.GeminiCLIModel },
		Call:         callGeminiCLI,
		Stream:       streamGeminiCLI,
	},
	{
		Name:         "codex-cli",
		Provider:     CodexCLI,
		DefaultModel: "gpt-5.5",
		Streaming:    true,
		ModelName:    func(cfg *Config) string { return cfg.CodexCLIModel },
		Call:         callCodexCLI,
		Stream:       streamCodexCLI,
	},
}

type templatePromptType string

const (
	templatePromptMR     templatePromptType = "mr"
	templatePromptCommit templatePromptType = "commit"
	templatePromptAny    templatePromptType = "any"
)

type templateMetadata struct {
	Name       string
	PromptType templatePromptType
}

var templateRegistry = []templateMetadata{
	{Name: "default", PromptType: templatePromptAny},
	{Name: "conventional", PromptType: templatePromptMR},
	{Name: "technical", PromptType: templatePromptMR},
	{Name: "user-focused", PromptType: templatePromptMR},
	{Name: "emoji", PromptType: templatePromptMR},
	{Name: "sassy", PromptType: templatePromptMR},
	{Name: "monday", PromptType: templatePromptMR},
	{Name: "jira", PromptType: templatePromptMR},
	{Name: "commit", PromptType: templatePromptCommit},
	{Name: "commit-emoji", PromptType: templatePromptCommit},
	{Name: "commit-conventional", PromptType: templatePromptCommit},
	{Name: "chaos", PromptType: templatePromptMR},
	{Name: "haiku", PromptType: templatePromptMR},
	{Name: "roast", PromptType: templatePromptMR},
	{Name: "intern", PromptType: templatePromptMR},
	{Name: "shakespeare", PromptType: templatePromptMR},
	{Name: "manager", PromptType: templatePromptMR},
	{Name: "yoda", PromptType: templatePromptMR},
	{Name: "excuse", PromptType: templatePromptMR},
}

var providerNames = providerCompletionValues()
var templateNames = templateCompletionValues()

func providerCompletionValues() []string {
	values := make([]string, 0, len(providerRegistry))
	for _, meta := range providerRegistry {
		values = append(values, meta.Name)
	}
	return values
}

func templateCompletionValues() []string {
	values := make([]string, 0, len(templateRegistry))
	for _, meta := range templateRegistry {
		values = append(values, meta.Name)
	}
	return values
}

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
	_, ok := providerInfo(provider)
	return ok
}

func providerInfo(provider ApiProvider) (providerMetadata, bool) {
	for _, meta := range providerRegistry {
		if provider == meta.Provider {
			return meta, true
		}
	}
	return providerMetadata{}, false
}

func templateInfo(name string) (templateMetadata, bool) {
	for _, meta := range templateRegistry {
		if meta.Name == name {
			return meta, true
		}
	}
	return templateMetadata{}, false
}

func isCommitOnlyTemplate(name string) bool {
	meta, ok := templateInfo(name)
	return ok && meta.PromptType == templatePromptCommit
}

func isMROnlyTemplate(name string) bool {
	meta, ok := templateInfo(name)
	return ok && meta.PromptType == templatePromptMR
}
