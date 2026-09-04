package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// providerSecrets maps each supported AI provider to the conventional GitHub
// Actions secret name used for its API key.
var providerSecrets = map[string]string{
	"openai":    "OPENAI_API_KEY",
	"anthropic": "ANTHROPIC_API_KEY",
	"gemini":    "GEMINI_API_KEY",
}

// allProviders is the ordered list used by check --all.
var allProviders = []ApiProvider{OpenAI, Anthropic, Gemini, Ollama, ClaudeCLI, GeminiCLI, CodexCLI}

const checkPingPrompt = `Reply with the single word: OK`

// pingResult holds the outcome of a single provider ping.
type pingResult struct {
	provider ApiProvider
	model    string
	elapsed  time.Duration
	err      error
	skipped  bool // true when credentials/binary are absent — not worth pinging
	skipMsg  string
}

// pingProvider sends a minimal prompt to one provider and returns the result.
func pingProvider(ctx context.Context, cfg *Config, provider ApiProvider, chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error)) pingResult {
	provCfg := *cfg
	provCfg.Provider = provider

	// Pre-flight: skip providers whose credentials/binary are clearly absent
	// so we don't waste time on a doomed network call.
	if err := validateProviderConfig(&provCfg); err != nil {
		return pingResult{provider: provider, model: getModelName(&provCfg), skipped: true, skipMsg: err.Error()}
	}

	start := time.Now()
	_, err := chatFn(ctx, &provCfg, provider, checkPingPrompt, "")
	return pingResult{
		provider: provider,
		model:    getModelName(&provCfg),
		elapsed:  time.Since(start),
		err:      err,
	}
}

// newCheckCmd returns a command that validates the configured provider is reachable
// by sending a minimal live ping and reporting the result.
func newCheckCmd(chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error)) *cobra.Command {
	var provider, modelOverride, profileName string
	var all bool

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Validate AI provider access with a live ping",
		Long: `Loads your configuration, prints the resolved settings, and sends a
minimal request to confirm the provider API or CLI is reachable and responding.

Use --all to ping every provider in parallel and print a summary table.

Examples:
  ai-mr-comment check
  ai-mr-comment check --provider anthropic
  ai-mr-comment check --all
  ai-mr-comment check --profile fast`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigForProfile(profileName)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()

			if all {
				if cancel := applyRequestTimeout(cmd, cfg); cancel != nil {
					defer cancel()
				}
				return runCheckAll(cmd.Context(), cfg, chatFn, out)
			}

			if cmd.Flags().Changed("provider") {
				cfg.Provider = ApiProvider(provider)
			}
			if cmd.Flags().Changed("model") {
				setModelOverride(cfg, modelOverride)
			}
			if !isSupportedProvider(cfg.Provider) {
				return withExitCode(4, errors.New("unsupported provider: "+string(cfg.Provider)))
			}

			// Print resolved config.
			_, _ = fmt.Fprintf(out, "Provider : %s\n", cfg.Provider)
			_, _ = fmt.Fprintf(out, "Model    : %s\n", getModelName(cfg))
			switch cfg.Provider {
			case OpenAI:
				_, _ = fmt.Fprintf(out, "Endpoint : %s\n", cfg.OpenAIEndpoint)
				_, _ = fmt.Fprintf(out, "API key  : %s\n", maskSecret(cfg.OpenAIAPIKey))
			case Anthropic:
				_, _ = fmt.Fprintf(out, "Endpoint : %s\n", cfg.AnthropicEndpoint)
				_, _ = fmt.Fprintf(out, "API key  : %s\n", maskSecret(cfg.AnthropicAPIKey))
			case Gemini:
				_, _ = fmt.Fprintf(out, "API key  : %s\n", maskSecret(cfg.GeminiAPIKey))
			case Ollama:
				_, _ = fmt.Fprintf(out, "Endpoint : %s\n", cfg.OllamaEndpoint)
			case ClaudeCLI:
				binary, binErr := findClaudeBinary(cfg)
				if binErr != nil {
					_, _ = fmt.Fprintf(out, "Binary   : (not found: %v)\n", binErr)
				} else {
					_, _ = fmt.Fprintf(out, "Binary   : %s\n", binary)
				}
			case GeminiCLI:
				binary, binErr := findGeminiCLIBinary(cfg)
				if binErr != nil {
					_, _ = fmt.Fprintf(out, "Binary   : (not found: %v)\n", binErr)
				} else {
					_, _ = fmt.Fprintf(out, "Binary   : %s\n", binary)
				}
			case CodexCLI:
				binary, binErr := findCodexBinary(cfg)
				if binErr != nil {
					_, _ = fmt.Fprintf(out, "Binary   : (not found: %v)\n", binErr)
				} else {
					_, _ = fmt.Fprintf(out, "Binary   : %s\n", binary)
				}
			}
			_, _ = fmt.Fprintln(out)

			// Validate config before attempting the live call.
			if cfgErr := validateProviderConfig(cfg); cfgErr != nil {
				return fmt.Errorf("config error: %w", cfgErr)
			}
			if cancel := applyRequestTimeout(cmd, cfg); cancel != nil {
				defer cancel()
			}

			_, _ = fmt.Fprintln(out, "Sending ping...")
			start := time.Now()
			reply, callErr := chatFn(cmd.Context(), cfg, cfg.Provider, checkPingPrompt, "")
			elapsed := time.Since(start)

			if callErr != nil {
				_, _ = fmt.Fprintf(out, "FAIL (%dms): %v\n", elapsed.Milliseconds(), callErr)
				return fmt.Errorf("check failed: %w", callErr)
			}

			reply = strings.TrimSpace(reply)
			_, _ = fmt.Fprintf(out, "OK (%dms)\n", elapsed.Milliseconds())
			_, _ = fmt.Fprintf(out, "Response : %s\n", reply)
			return nil
		},
	}

	cmd.Flags().StringVar(&provider, "provider", "", "AI provider to check (overrides config)")
	cmd.Flags().StringVar(&modelOverride, "model", "", "Override the model for this check")
	cmd.Flags().StringVar(&profileName, "profile", "", "Named config profile to activate")
	cmd.Flags().BoolVar(&all, "all", false, "Ping every provider in parallel and print a summary table")
	_ = cmd.RegisterFlagCompletionFunc("provider", completeValues(providerNames))
	_ = cmd.RegisterFlagCompletionFunc("profile", completeProfiles)
	return cmd
}

func newDoctorCmd() *cobra.Command {
	var provider, modelOverride, profileName, presetName, format string

	cmd := &cobra.Command{
		Use:     "doctor",
		Aliases: []string{"config-dump"},
		Short:   "Inspect resolved config and local CLI readiness without a live provider call",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigForProfile(profileName)
			if err != nil {
				return err
			}
			dummyExitCode := false
			dummyPlain := false
			dummyTitle := false
			if _, presetErr := applyRootPreset(cmd, presetName, cfg, &format, &dummyExitCode, &dummyPlain, &dummyTitle); presetErr != nil {
				return withExitCode(4, presetErr)
			}
			if cmd.Flags().Changed("provider") {
				cfg.Provider = ApiProvider(provider)
			}
			if cmd.Flags().Changed("model") {
				setModelOverride(cfg, modelOverride)
			}
			if !isSupportedProvider(cfg.Provider) {
				return withExitCode(4, errors.New("unsupported provider: "+string(cfg.Provider)))
			}
			if format != "text" && format != "json" {
				return withExitCode(4, fmt.Errorf("unsupported format %q: must be text or json", format))
			}

			type doctorPayload struct {
				ConfigFile string            `json:"config_file"`
				Profile    string            `json:"profile,omitempty"`
				Preset     string            `json:"preset,omitempty"`
				Provider   string            `json:"provider"`
				Model      string            `json:"model"`
				Template   string            `json:"template"`
				Timeout    string            `json:"request_timeout"`
				Git        map[string]string `json:"git"`
				Secrets    map[string]string `json:"secrets"`
				Binaries   map[string]string `json:"binaries"`
			}

			gitInfo := map[string]string{"repository": "false"}
			if isGitRepo() {
				gitInfo["repository"] = "true"
				if branch, branchErr := getCurrentBranch(); branchErr == nil && branch != "" {
					gitInfo["branch"] = branch
				}
				if remote, remoteErr := getRemoteURL(); remoteErr == nil && remote != "" {
					gitInfo["remote"] = sanitizeRemoteURL(remote)
				}
			}
			binaries := map[string]string{}
			if binary, binErr := findClaudeBinary(cfg); binErr == nil {
				binaries["claude-cli"] = binary
			} else {
				binaries["claude-cli"] = "(not found)"
			}
			if binary, binErr := findGeminiCLIBinary(cfg); binErr == nil {
				binaries["gemini-cli"] = binary
			} else {
				binaries["gemini-cli"] = "(not found)"
			}
			if binary, binErr := findCodexBinary(cfg); binErr == nil {
				binaries["codex-cli"] = binary
			} else {
				binaries["codex-cli"] = "(not found)"
			}
			configFile := cfg.ConfigFile
			if configFile == "" {
				configFile = "(none)"
			}
			payload := doctorPayload{
				ConfigFile: configFile,
				Profile:    profileName,
				Preset:     presetName,
				Provider:   string(cfg.Provider),
				Model:      getModelName(cfg),
				Template:   cfg.Template,
				Timeout:    cfg.RequestTimeout.String(),
				Git:        gitInfo,
				Secrets: map[string]string{
					"OPENAI_API_KEY":    secretStatus(cfg.OpenAIAPIKey),
					"ANTHROPIC_API_KEY": secretStatus(cfg.AnthropicAPIKey),
					"GEMINI_API_KEY":    secretStatus(cfg.GeminiAPIKey),
					"GITHUB_TOKEN":      secretStatus(cfg.GitHubToken),
					"GITLAB_TOKEN":      secretStatus(cfg.GitLabToken),
				},
				Binaries: binaries,
			}
			out := cmd.OutOrStdout()
			if format == "json" {
				return json.NewEncoder(out).Encode(payload)
			}
			_, _ = fmt.Fprintf(out, "Config file : %s\n", payload.ConfigFile)
			if profileName != "" {
				_, _ = fmt.Fprintf(out, "Profile     : %s\n", profileName)
			}
			if presetName != "" {
				_, _ = fmt.Fprintf(out, "Preset      : %s\n", presetName)
			}
			_, _ = fmt.Fprintf(out, "Provider    : %s\n", payload.Provider)
			_, _ = fmt.Fprintf(out, "Model       : %s\n", payload.Model)
			_, _ = fmt.Fprintf(out, "Template    : %s\n", payload.Template)
			_, _ = fmt.Fprintf(out, "Timeout     : %s\n", payload.Timeout)
			_, _ = fmt.Fprintf(out, "Git repo    : %s\n", payload.Git["repository"])
			if payload.Git["branch"] != "" {
				_, _ = fmt.Fprintf(out, "Git branch  : %s\n", payload.Git["branch"])
			}
			_, _ = fmt.Fprintln(out, "\nSecrets:")
			for _, name := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GEMINI_API_KEY", "GITHUB_TOKEN", "GITLAB_TOKEN"} {
				_, _ = fmt.Fprintf(out, "- %s: %s\n", name, payload.Secrets[name])
			}
			_, _ = fmt.Fprintln(out, "\nCLI binaries:")
			for _, name := range []string{"claude-cli", "gemini-cli", "codex-cli"} {
				_, _ = fmt.Fprintf(out, "- %s: %s\n", name, payload.Binaries[name])
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "", "AI provider to inspect (overrides config)")
	cmd.Flags().StringVar(&modelOverride, "model", "", "Override the model for inspection")
	cmd.Flags().StringVar(&profileName, "profile", "", "Named config profile to activate")
	cmd.Flags().StringVar(&presetName, "preset", "", "Preset defaults: ci, local-fast, security, release-notes")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text or json")
	_ = cmd.RegisterFlagCompletionFunc("provider", completeValues(providerNames))
	_ = cmd.RegisterFlagCompletionFunc("profile", completeProfiles)
	_ = cmd.RegisterFlagCompletionFunc("preset", completeValues(presetNames))
	_ = cmd.RegisterFlagCompletionFunc("format", completeValues([]string{"text", "json"}))
	return cmd
}

func secretStatus(value string) string {
	if value == "" {
		return "missing"
	}
	return "set"
}

// runCheckAll pings all providers concurrently and prints a summary table.
// Returns an error if any configured (non-skipped) provider fails.
func runCheckAll(ctx context.Context, cfg *Config, chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error), out io.Writer) error {
	_, _ = fmt.Fprintln(out, "Pinging all providers...")
	_, _ = fmt.Fprintln(out)

	type indexedResult struct {
		idx int
		pingResult
	}

	results := make([]pingResult, len(allProviders))
	ch := make(chan indexedResult, len(allProviders))

	for i, p := range allProviders {
		i, p := i, p
		go func() {
			ch <- indexedResult{idx: i, pingResult: pingProvider(ctx, cfg, p, chatFn)}
		}()
	}
	for range allProviders {
		r := <-ch
		results[r.idx] = r.pingResult
	}

	// Print aligned table.
	const colProvider = 12
	const colModel = 24
	_, _ = fmt.Fprintf(out, "%-*s  %-*s  %s\n", colProvider, "PROVIDER", colModel, "MODEL", "STATUS")
	_, _ = fmt.Fprintf(out, "%s  %s  %s\n", strings.Repeat("-", colProvider), strings.Repeat("-", colModel), strings.Repeat("-", 20))

	var anyFailed bool
	for _, r := range results {
		var status string
		switch {
		case r.skipped:
			status = "SKIP — " + firstLine(r.skipMsg)
		case r.err != nil:
			anyFailed = true
			status = fmt.Sprintf("FAIL (%dms) — %s", r.elapsed.Milliseconds(), firstLine(r.err.Error()))
		default:
			status = fmt.Sprintf("OK   (%dms)", r.elapsed.Milliseconds())
		}
		_, _ = fmt.Fprintf(out, "%-*s  %-*s  %s\n", colProvider, r.provider, colModel, r.model, status)
	}
	if anyFailed {
		_, _ = fmt.Fprintf(out, "  tip: run 'check --provider <name>' for full error details\n")
	}

	_, _ = fmt.Fprintln(out)
	if anyFailed {
		return errors.New("one or more providers failed — see table above")
	}
	return nil
}

// firstLine returns the text up to the first newline, trimmed, and truncated
// to maxLen runes with "…" appended when exceeded.
func firstLine(s string) string {
	const maxLen = 72
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len([]rune(s)) > maxLen {
		return string([]rune(s)[:maxLen]) + "…"
	}
	return s
}

// maskSecret returns the first 4 characters of s followed by "****", or
// "(not set)" when s is empty.
func maskSecret(s string) string {
	if s == "" {
		return "(not set)"
	}
	if len(s) <= 4 {
		return "****"
	}
	return s[:4] + "****"
}
