package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// providerModels lists known models for each provider, used by the `models` subcommand.
var providerModels = map[ApiProvider][]string{
	OpenAI: {
		"gpt-6-astra", "gpt-6.1-sol", "gpt-6.1-terra", "gpt-6-luna", "gpt-6-sol",
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
		"claude-fable-5-1", "claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5",
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
		"gemini-4-argon", "gemini-3.8-flash", "gemini-3.6-flash", "gemini-3.5-flash-lite",
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
		"claude-fable-5-1", "claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5",
		"claude-opus-4-7",
		"claude-opus-4-6",
		"claude-sonnet-4-6",
		"claude-haiku-4-5-20251001",
	},
	GeminiCLI: {
		"gemini-4-argon", "gemini-3.8-flash", "gemini-3.6-flash", "gemini-3.5-flash-lite",
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
	var profileName, format string
	var refresh bool

	cmd := &cobra.Command{
		Use:   "models",
		Args:  cobra.NoArgs,
		Short: "List available models for a provider",
		Long:  `Prints the known model names for the given provider. Use --provider to select a provider (defaults to the configured one).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if format != "text" && format != "json" {
				return fmt.Errorf("unsupported format %q: must be text or json", format)
			}
			p := ApiProvider(provider)
			var cfg *Config
			if !cmd.Flags().Changed("provider") || refresh {
				var err error
				cfg, err = loadConfigForProfile(profileName)
				if err != nil {
					return err
				}
				if !cmd.Flags().Changed("provider") {
					p = cfg.Provider
				}
			}
			models, ok := providerModels[p]
			if !ok {
				return fmt.Errorf("unknown provider %q: choose from openai, anthropic, gemini, ollama, claude-cli, gemini-cli, codex-cli", p)
			}
			source := "builtin"
			if refresh {
				if p != Ollama {
					return fmt.Errorf("--refresh is supported for the ollama provider")
				}
				var err error
				models, err = fetchOllamaModels(cmd.Context(), cfg)
				if err != nil {
					return err
				}
				source = "ollama"
			}
			if format == "json" {
				if models == nil {
					models = []string{}
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
					Provider ApiProvider `json:"provider"`
					Models   []string    `json:"models"`
					Source   string      `json:"source"`
				}{p, models, source})
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Models for provider %s:\n\n", p)
			if len(models) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  (no fixed model list — configured by the local CLI tool)")
			}
			for _, m := range models {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", m)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Use --model <name> to select any model ID supported by your provider.\n")
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "text", "Output format: text or json")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "Query installed models from the configured Ollama server")
	_ = cmd.RegisterFlagCompletionFunc("format", completeValues([]string{"text", "json"}))
	cmd.Flags().StringVar(&provider, "provider", "", "Provider to list models for (openai, anthropic, gemini, ollama, claude-cli, gemini-cli, codex-cli)")
	cmd.Flags().StringVar(&profileName, "profile", "", "Named config profile to activate")
	_ = cmd.RegisterFlagCompletionFunc("provider", completeValues(providerNames))
	_ = cmd.RegisterFlagCompletionFunc("profile", completeProfiles)
	return cmd
}

func fetchOllamaModels(ctx context.Context, cfg *Config) ([]string, error) {
	endpoint, err := url.Parse(cfg.OllamaEndpoint)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" {
		return nil, fmt.Errorf("invalid Ollama endpoint")
	}
	path := strings.TrimSuffix(endpoint.Path, "/")
	if prefix, ok := strings.CutSuffix(path, "/api/generate"); ok {
		path = prefix
	}
	if prefix, ok := strings.CutSuffix(path, "/api/chat"); ok {
		path = prefix
	}
	endpoint.Path = strings.TrimSuffix(path, "/") + "/api/tags"
	endpoint.RawPath = ""
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	timeout := 10 * time.Second
	if cfg.RequestTimeout > 0 {
		timeout = cfg.RequestTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing Ollama models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listing Ollama models: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 4<<20 {
		return nil, fmt.Errorf("ollama model list exceeds 4 MiB")
	}
	var payload struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decoding Ollama model list: %w", err)
	}
	seen := map[string]bool{}
	names := []string{}
	for _, model := range payload.Models {
		name := strings.TrimSpace(model.Name)
		if name == "" {
			name = strings.TrimSpace(model.Model)
		}
		if name != "" && !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	sort.Strings(names)
	return names, nil
}

func completeModels(cmd *cobra.Command, _ []string, prefix string) ([]string, cobra.ShellCompDirective) {
	profile, _ := cmd.Flags().GetString("profile")
	cfg, err := loadConfigForProfile(profile)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	p := cfg.Provider
	if provider, _ := cmd.Flags().GetString("provider"); provider != "" {
		p = ApiProvider(provider)
	}
	return completeValues(providerModels[p])(cmd, nil, prefix)
}
