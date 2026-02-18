// Package main is the entry point for ai-mr-comment, a CLI tool that generates
// MR/PR comments from git diffs using AI providers.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func main() {
	if err := newRootCmd(chatCompletions).Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// newRootCmd builds the root cobra command, wiring flags to the provided chatFn.
// Accepting chatFn as a parameter allows tests to inject a mock without real API calls.
func newRootCmd(chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error)) *cobra.Command {
	var commit, diffFilePath, outputPath, provider, templateName, format string
	var debug, staged, clipboardFlag, smartChunk, generateTitle bool
	var exclude []string

	rootCmd := &cobra.Command{
		Use:           "ai-mr-comment",
		Short:         "Generate MR/PR comments using AI",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := loadConfig()
			if cmd.Flags().Changed("provider") {
				cfg.Provider = ApiProvider(provider)
			}
			if cmd.Flags().Changed("template") {
				cfg.Template = templateName
			}

			if cfg.Provider != OpenAI && cfg.Provider != Anthropic && cfg.Provider != Ollama && cfg.Provider != Gemini {
				return errors.New("unsupported provider: " + string(cfg.Provider))
			}

			if cfg.Provider == OpenAI && cfg.OpenAIAPIKey == "" {
				return fmt.Errorf("missing OpenAI API key.\n\n" +
					"Please set the OPENAI_API_KEY environment variable or configure 'openai_api_key' in ~/.ai-mr-comment.toml")
			}
			if cfg.Provider == Anthropic && cfg.AnthropicAPIKey == "" {
				return fmt.Errorf("missing Anthropic API key.\n\n" +
					"Please set the ANTHROPIC_API_KEY environment variable or configure 'anthropic_api_key' in ~/.ai-mr-comment.toml")
			}
			if cfg.Provider == Gemini && cfg.GeminiAPIKey == "" {
				return fmt.Errorf("missing Gemini API key.\n\n" +
					"Please set the GEMINI_API_KEY environment variable or configure 'gemini_api_key' in ~/.ai-mr-comment.toml")
			}

			if staged && commit != "" {
				return errors.New("--staged and --commit are mutually exclusive")
			}

			if format != "text" && format != "json" {
				return fmt.Errorf("unsupported format %q: must be text or json", format)
			}

			var diffContent string
			var err error
			if diffFilePath != "" {
				diffContent, err = readDiffFromFile(diffFilePath)
			} else {
				if !isGitRepo() {
					return fmt.Errorf("not a git repository. Run from inside a git repo or use --file to provide a diff")
				}
				diffContent, err = getGitDiff(commit, staged, exclude)
			}
			if err != nil {
				return err
			}
			if strings.TrimSpace(diffContent) == "" {
				if staged {
					return fmt.Errorf("no staged changes found. Stage your changes with 'git add' first")
				}
				return fmt.Errorf("no diff found. Make sure you have uncommitted changes or specify a commit range with --commit")
			}

			out := cmd.OutOrStdout()
			diffContent = processDiff(diffContent, 4000)
			systemPrompt, err := NewPromptTemplate(cfg.Template)
			if err != nil {
				_, _ = fmt.Fprintln(os.Stderr, "Warning:", err)
			}

			if debug {
				estimator := NewTokenEstimator(cfg)
				model := getModelName(cfg)

				// Gemini uses the official SDK for exact counts; others use a heuristic.
				totalTokens, err := estimator.CountTokens(cmd.Context(), model, systemPrompt, diffContent)
				if err != nil {
					_, _ = fmt.Fprintf(out, "Error estimating tokens: %v\n", err)
					fallback := &HeuristicTokenEstimator{}
					totalTokens, _ = fallback.CountTokens(context.Background(), "", systemPrompt, diffContent)
					_, _ = fmt.Fprintln(out, "Using heuristic fallback.")
				}

				originalLen := len(strings.Split(diffContent, "\n"))
				cost := EstimateCost(model, totalTokens)

				_, _ = fmt.Fprintln(out, "Token & Cost Estimation:")
				_, _ = fmt.Fprintf(out, "- Model: %s\n", model)
				_, _ = fmt.Fprintf(out, "- Diff lines: %d\n", originalLen)
				_, _ = fmt.Fprintf(out, "- Estimated Input Tokens: %d\n", totalTokens)
				_, _ = fmt.Fprintf(out, "- Estimated Input Cost: $%.6f\n", cost)
				_, _ = fmt.Fprintln(out, "\nNote: Output tokens and cost depend on the generated response length.")
				return nil
			}

			// Stream tokens directly to the terminal when output is a real TTY,
			// text format is selected, smart-chunk is off, and no output file is set.
			// All other paths use the buffered chatFn to get the full response first.
			isTTY := term.IsTerminal(int(os.Stdout.Fd()))
			shouldStream := isTTY && format == "text" && !smartChunk && outputPath == ""
			// streamedOK is set to true only when streaming completes successfully.
			// The output block uses it to decide whether body was already written.
			var streamedOK bool

			var comment string
			if smartChunk {
				chunks := splitDiffByFile(diffContent)
				if len(chunks) > 1 {
					// Summarize each file chunk independently, then do a final synthesis call.
					const chunkPrompt = "Summarize the changes in this file diff in 3-5 bullet points. Be concise and technical."
					var summaries []string
					for _, chunk := range chunks {
						summary, chunkErr := chatFn(cmd.Context(), cfg, cfg.Provider, chunkPrompt, processDiff(chunk, 1000))
						if chunkErr != nil {
							if cfg.Provider == Ollama && strings.Contains(chunkErr.Error(), "connection refused") {
								return fmt.Errorf("failed to connect to Ollama at %s.\nMake sure Ollama is running (try 'ollama serve') or check your configuration", cfg.OllamaEndpoint)
							}
							return chunkErr
						}
						summaries = append(summaries, summary)
					}
					combinedSummaries := strings.Join(summaries, "\n\n---\n\n")
					comment, err = chatFn(cmd.Context(), cfg, cfg.Provider, systemPrompt, combinedSummaries)
				} else {
					comment, err = chatFn(cmd.Context(), cfg, cfg.Provider, systemPrompt, diffContent)
				}
			} else if shouldStream {
				comment, err = streamToWriter(cmd.Context(), cfg, cfg.Provider, systemPrompt, diffContent, out)
				if err != nil {
					// Streaming failed; fall back to the buffered call.
					// headerPrinted=true tells the output block to skip reprinting the separator.
					comment, err = chatFn(cmd.Context(), cfg, cfg.Provider, systemPrompt, diffContent)
				} else {
					streamedOK = true
				}
			} else {
				comment, err = chatFn(cmd.Context(), cfg, cfg.Provider, systemPrompt, diffContent)
			}
			if err != nil {
				if cfg.Provider == Ollama && strings.Contains(err.Error(), "connection refused") {
					return fmt.Errorf("failed to connect to Ollama at %s.\nMake sure Ollama is running (try 'ollama serve') or check your configuration", cfg.OllamaEndpoint)
				}
				return err
			}

			// Optionally generate a title with a separate focused API call.
			var title string
			if generateTitle {
				title, err = chatFn(cmd.Context(), cfg, cfg.Provider, titlePrompt, diffContent)
				if err != nil {
					if cfg.Provider == Ollama && strings.Contains(err.Error(), "connection refused") {
						return fmt.Errorf("failed to connect to Ollama at %s.\nMake sure Ollama is running (try 'ollama serve') or check your configuration", cfg.OllamaEndpoint)
					}
					return err
				}
				title = strings.TrimSpace(title)
			}

			if format == "json" {
				// outputJSON is the structured response emitted when --format=json is set.
				type outputJSON struct {
					Title    string `json:"title,omitempty"`
					Comment  string `json:"comment"`
					Provider string `json:"provider"`
					Model    string `json:"model"`
				}
				if err := json.NewEncoder(out).Encode(outputJSON{
					Title:    title,
					Comment:  comment,
					Provider: string(cfg.Provider),
					Model:    getModelName(cfg),
				}); err != nil {
					return err
				}
			} else if streamedOK {
				// Streaming succeeded: body was already written token-by-token.
				// Print a trailing newline and the title if generated.
				_, _ = fmt.Fprintln(out)
				if title != "" {
					_, _ = fmt.Fprintln(out, title)
					_, _ = fmt.Fprintln(out)
				}
			} else {
				_, _ = fmt.Fprintln(out)
				_, _ = fmt.Fprintln(out, "----------------------------------------")
				_, _ = fmt.Fprintln(out)
				if title != "" {
					_, _ = fmt.Fprintln(out, title)
					_, _ = fmt.Fprintln(out)
				}
				_, _ = fmt.Fprintln(out, comment)
			}

			if clipboardFlag {
				if err := clipboard.WriteAll(comment); err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "Warning: could not copy to clipboard: %v\n", err)
				}
			}

			if outputPath != "" {
				return os.WriteFile(outputPath, []byte(comment), 0644)
			}
			return nil
		},
	}

	rootCmd.Flags().StringVar(&commit, "commit", "", "Commit or commit range")
	rootCmd.Flags().StringVar(&diffFilePath, "file", "", "Path to diff file")
	rootCmd.Flags().StringVar(&outputPath, "output", "", "Output file path")
	rootCmd.Flags().StringVar(&provider, "provider", "openai", "API provider (openai, anthropic, gemini, ollama)")
	rootCmd.Flags().StringVarP(&templateName, "template", "t", "default", "Prompt template to use (e.g., default, conventional, technical)")
	rootCmd.Flags().BoolVar(&debug, "debug", false, "Estimate token usage")
	rootCmd.Flags().BoolVar(&staged, "staged", false, "Diff staged changes only (git diff --cached)")
	rootCmd.Flags().BoolVar(&clipboardFlag, "clipboard", false, "Copy output to clipboard")
	rootCmd.Flags().StringArrayVar(&exclude, "exclude", nil, "Exclude files matching pattern (e.g. vendor/**, *.sum). Can be repeated.")
	rootCmd.Flags().StringVar(&format, "format", "text", "Output format: text or json")
	rootCmd.Flags().BoolVar(&smartChunk, "smart-chunk", false, "Split large diffs by file, summarize each, then combine")
	rootCmd.Flags().BoolVar(&generateTitle, "title", false, "Generate a concise MR/PR title in addition to the comment")

	rootCmd.AddCommand(newInitConfigCmd())

	rootCmd.AddCommand(&cobra.Command{
		Use:       "completion [bash|zsh|fish|powershell]",
		Short:     "Generate shell completion script",
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return rootCmd.GenBashCompletionV2(os.Stdout, true)
			case "zsh":
				return rootCmd.GenZshCompletion(os.Stdout)
			case "fish":
				return rootCmd.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
			default:
				return fmt.Errorf("unsupported shell: %s", args[0])
			}
		},
	})

	return rootCmd
}

// defaultConfigTOML is the template written by the init-config subcommand.
// It documents every supported key with its default value.
const defaultConfigTOML = `# ai-mr-comment configuration
# Place this file at ~/.ai-mr-comment.toml or in the project root.

# Default AI provider: openai | anthropic | gemini | ollama
provider = "openai"

# Default prompt template: default | conventional | technical
template = "default"

# --- OpenAI ---
# openai_api_key = ""   # or set OPENAI_API_KEY env var
openai_model    = "gpt-4o-mini"
openai_endpoint = "https://api.openai.com/v1/chat/completions"

# --- Anthropic ---
# anthropic_api_key = ""   # or set ANTHROPIC_API_KEY env var
anthropic_model    = "claude-3-5-sonnet-20240620"
anthropic_endpoint = "https://api.anthropic.com/v1/messages"

# --- Google Gemini ---
# gemini_api_key = ""   # or set GEMINI_API_KEY env var
gemini_model = "gemini-2.5-flash"

# --- Ollama (local) ---
ollama_model    = "llama3"
ollama_endpoint = "http://localhost:11434/api/generate"
`

// newInitConfigCmd returns the init-config subcommand, which writes a commented
// TOML configuration file to the destination path (default: ~/.ai-mr-comment.toml).
func newInitConfigCmd() *cobra.Command {
	var outputPath string

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
	return cmd
}

// getModelName returns the configured model name for the active provider.
func getModelName(cfg *Config) string {
	switch cfg.Provider {
	case OpenAI:
		return cfg.OpenAIModel
	case Anthropic:
		return cfg.AnthropicModel
	case Gemini:
		return cfg.GeminiModel
	case Ollama:
		return cfg.OllamaModel
	default:
		return "unknown"
	}
}
