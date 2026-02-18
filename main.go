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
		Use:   "ai-mr-comment",
		Short: "Generate MR/PR comments using AI",
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
				diffContent, err = getGitDiff(commit, staged, exclude)
			}
			if err != nil {
				return err
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
