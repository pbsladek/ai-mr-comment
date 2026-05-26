package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// defaultConfigTOML is the template written by the init-config subcommand.
// It documents every supported key with its default value.
const defaultConfigTOML = `# ai-mr-comment configuration
# Place this file at ~/.ai-mr-comment.toml or in the project root.

# Default AI provider: openai | anthropic | gemini | ollama | claude-cli | gemini-cli | codex-cli
provider = "anthropic"

# Default prompt template.
# Built-in options: default | conventional | technical | user-focused | emoji | sassy | monday
# You can also create custom templates in ~/.config/ai-mr-comment/templates/<name>.tmpl
template = "default"

# Optional timeout for network/CLI provider calls. Use Go duration syntax, e.g. "2m" or "30s".
# "0s" disables the timeout and preserves the provider/client default behavior.
request_timeout = "0s"

# --- OpenAI ---
# openai_api_key = ""   # or set OPENAI_API_KEY env var
openai_model    = "gpt-5.5"
openai_endpoint = "https://api.openai.com/v1/"
# Other OpenAI models: gpt-5.5-pro, gpt-5.4, gpt-5.4-pro, gpt-5.4-mini, gpt-5.4-nano, gpt-5.3-codex

# --- Anthropic ---
# anthropic_api_key = ""   # or set ANTHROPIC_API_KEY env var
anthropic_model    = "claude-sonnet-4-6"
anthropic_endpoint = "https://api.anthropic.com/"
# Other Anthropic models: claude-opus-4-7, claude-opus-4-6, claude-haiku-4-5-20251001

# --- Google Gemini ---
# gemini_api_key = ""   # or set GEMINI_API_KEY env var
gemini_model = "gemini-2.5-flash"
# Other Gemini models: gemini-3.1-pro-preview, gemini-3-flash-preview, gemini-3.1-flash-lite, gemini-2.5-pro, gemini-2.5-flash-lite

# --- Ollama (local) ---
ollama_model    = "llama3.2"
ollama_endpoint = "http://localhost:11434/api/generate"
# Other Ollama models: llama3.1, llama3, mistral, codellama, phi3

# --- Claude CLI (claude-cli) ---
# Uses the local claude CLI for auth — no API key required.
# Auth is delegated to the claude CLI process (e.g. Claude Code session).
# claude_cli_path = ""              # auto-detected: ~/.claude/local/claude, then PATH
claude_cli_model = "claude-sonnet-4-6"

# --- Gemini CLI (gemini-cli) ---
# Uses the local gemini CLI with Google OAuth — no API key required.
# Install: npm install -g @google/gemini-cli
# gemini_cli_path = ""              # auto-detected via PATH
gemini_cli_model = "gemini-2.5-flash"

# --- Codex CLI (codex-cli) ---
# Uses the local OpenAI Codex CLI in quiet mode — requires OPENAI_API_KEY.
# Install: npm install -g @openai/codex
# codex_cli_path = ""               # auto-detected via PATH
# codex_cli_model = ""              # leave empty to use codex default

# --- GitHub / GitHub Enterprise ---
# github_token = ""    # or set GITHUB_TOKEN env var
# github_base_url = "" # GitHub Enterprise host, e.g. https://github.mycompany.com

# --- GitLab / Self-Hosted GitLab ---
# gitlab_token = ""    # or set GITLAB_TOKEN env var
# gitlab_base_url = "" # Self-hosted GitLab host, e.g. https://gitlab.mycompany.com

# ---------------------------------------------------------------------------
# Named Profiles
# Switch profiles with: ai-mr-comment --profile <name>
# A profile overrides top-level file settings for that invocation only.
# Environment variables still take precedence over profile values.
# ---------------------------------------------------------------------------

# Fast / cheap — gpt-5.4-nano for quick reviews and commit messages
[profile.fast]
provider     = "openai"
openai_model = "gpt-5.4-nano"
template     = "conventional"

# OpenAI — gpt-5.5 with technical template for thorough reviews
[profile.openai]
provider     = "openai"
openai_model = "gpt-5.5"
template     = "technical"

# Anthropic — claude-opus-4-7 with technical template
[profile.anthropic]
provider        = "anthropic"
anthropic_model = "claude-opus-4-7"
template        = "technical"

# Gemini — gemini-3.1-pro-preview with technical template
[profile.gemini]
provider     = "gemini"
gemini_model = "gemini-3.1-pro-preview"
template     = "technical"

# Local / offline — Ollama, no API key required
[profile.local]
provider     = "ollama"
ollama_model = "llama3.2"
template     = "default"

# Claude CLI — delegates auth to the local claude CLI (Claude Code session)
[profile.claude-cli]
provider         = "claude-cli"
claude_cli_model = "claude-sonnet-4-6"
template         = "default"

# Gemini CLI — delegates auth to the local gemini CLI (Google OAuth)
[profile.gemini-cli]
provider         = "gemini-cli"
gemini_cli_model = "gemini-2.5-flash"
template         = "default"

# Codex CLI — uses the local codex CLI (requires OPENAI_API_KEY)
[profile.codex-cli]
provider = "codex-cli"
template = "default"
`

// newInitConfigCmd returns the init-config subcommand, which writes a commented
// TOML configuration file to the destination path (default: ~/.ai-mr-comment.toml).
func newInitConfigCmd() *cobra.Command {
	var outputPath string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "init-config",
		Short: "Write a default config file to ~/.ai-mr-comment.toml",
		Long: `Writes a commented TOML configuration file with all supported settings and
their defaults. Edit the generated file to add your API keys and customise
models, endpoints, or the default provider.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dest := outputPath
			if dest == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("could not determine home directory: %w", err)
				}
				dest = home + "/.ai-mr-comment.toml"
			}

			if dryRun {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Dry run: would write config file to %s\n", dest)
				return nil
			}

			if _, err := os.Stat(dest); err == nil {
				return fmt.Errorf("config file already exists at %s (remove it first or use --output to choose a different path)", dest)
			}

			if err := os.WriteFile(dest, []byte(defaultConfigTOML), 0600); err != nil {
				return fmt.Errorf("could not write config file: %w", err)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Config file written to %s\n", dest)
			return nil
		},
	}

	cmd.Flags().StringVar(&outputPath, "output", "", "Write config to this path instead of ~/.ai-mr-comment.toml")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print destination without writing the config file")
	return cmd
}

// getModelName returns the configured model name for the active provider.
func getModelName(cfg *Config) string {
	if meta, ok := providerInfo(cfg.Provider); ok && meta.ModelName != nil {
		return meta.ModelName(cfg)
	}
	return "unknown"
}

func validateProviderConfig(cfg *Config) error {
	if !isSupportedProvider(cfg.Provider) {
		return errors.New("unsupported provider: " + string(cfg.Provider))
	}
	// Delegate API key validation to validateAPIKey (same check used by chatCompletions).
	return validateAPIKey(cfg.Provider, cfg)
}

func applyRequestTimeout(cmd *cobra.Command, cfg *Config) context.CancelFunc {
	if cfg.RequestTimeout <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), cfg.RequestTimeout)
	cmd.SetContext(ctx)
	return cancel
}

// debugLog writes a formatted debug message to cfg.DebugWriter when verbose mode is enabled.
// The message is prefixed with "[debug] " and terminated with a newline.
func debugLog(cfg *Config, format string, args ...any) {
	if cfg.DebugWriter == nil {
		return
	}
	debugWriterMu.Lock()
	defer debugWriterMu.Unlock()
	_, _ = fmt.Fprintf(cfg.DebugWriter, "[debug] "+format+"\n", args...)
}

// timedCall invokes fn, then logs the elapsed time, response size, and any error.
// It is a no-op when verbose mode is disabled (cfg.DebugWriter == nil).
func timedCall(cfg *Config, label string, fn func() (string, error)) (string, error) {
	start := time.Now()
	result, err := fn()
	elapsed := time.Since(start).Milliseconds()
	if err == nil {
		debugLog(cfg, "api: %s completed in %dms chars=%d lines=%d",
			label, elapsed, len(result), len(strings.Split(result, "\n")))
	} else {
		debugLog(cfg, "api: %s failed in %dms: %v", label, elapsed, err)
	}
	return result, err
}

// showCostEstimate prints token and cost estimation to w.
func showCostEstimate(ctx context.Context, cfg *Config, systemPrompt, diffContent string, w io.Writer) {
	model := getModelName(cfg)
	estimator := NewTokenEstimator(cfg)
	totalTokens, err := estimator.CountTokens(ctx, model, systemPrompt, diffContent)
	if err != nil {
		_, _ = fmt.Fprintf(w, "Error estimating tokens: %v\n", err)
		fallback := &HeuristicTokenEstimator{}
		totalTokens, _ = fallback.CountTokens(context.Background(), "", systemPrompt, diffContent)
		_, _ = fmt.Fprintln(w, "Using heuristic fallback.")
	}
	cost := EstimateCost(model, totalTokens)
	_, _ = fmt.Fprintln(w, "Token & Cost Estimation:")
	_, _ = fmt.Fprintf(w, "- Model: %s\n", model)
	_, _ = fmt.Fprintf(w, "- Diff lines: %d\n", strings.Count(diffContent, "\n")+1)
	_, _ = fmt.Fprintf(w, "- Estimated Input Tokens: %d\n", totalTokens)
	_, _ = fmt.Fprintf(w, "- Estimated Input Cost: $%.6f\n", cost)
	_, _ = fmt.Fprintln(w, "\nNote: Output tokens and cost depend on the generated response length.")
}

// promptConfirm writes a "Proceed? [y/N]: " prompt to promptWriter and reads
// one line from stdinReader. Returns true only if the user types "y" or "Y".
// Auto-confirms when autoYes is true. Auto-declines when stdinReader is not
// an interactive terminal (e.g. in CI or piped input).
func promptConfirm(promptWriter io.Writer, stdinReader io.Reader, autoYes bool) bool {
	if autoYes {
		return true
	}
	if f, ok := stdinReader.(*os.File); ok {
		if !fileIsTerminal(f) {
			_, _ = fmt.Fprintln(promptWriter, "Non-interactive mode: auto-declining. Use --yes to proceed.")
			return false
		}
	} else {
		// Non-*os.File reader (e.g. strings.Reader in tests) is non-interactive.
		_, _ = fmt.Fprintln(promptWriter, "Non-interactive mode: auto-declining. Use --yes to proceed.")
		return false
	}
	_, _ = fmt.Fprint(promptWriter, "Proceed? [y/N]: ")
	var line string
	_, _ = fmt.Fscan(stdinReader, &line)
	return strings.ToLower(strings.TrimSpace(line)) == "y"
}

func fileIsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd())) //nolint:gosec // G115: os.File descriptors are small OS-provided handles expected by x/term.
}

// setModelOverride applies a CLI --model value to the correct provider field in cfg.
func setModelOverride(cfg *Config, model string) {
	switch cfg.Provider {
	case OpenAI:
		cfg.OpenAIModel = model
	case Anthropic:
		cfg.AnthropicModel = model
	case Gemini:
		cfg.GeminiModel = model
	case Ollama:
		cfg.OllamaModel = model
	case ClaudeCLI:
		cfg.ClaudeCLIModel = model
	case GeminiCLI:
		cfg.GeminiCLIModel = model
	case CodexCLI:
		cfg.CodexCLIModel = model
	}
}
