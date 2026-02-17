package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCmd(chatCompletions).Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func newRootCmd(chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error)) *cobra.Command {
	var commit, diffFilePath, outputPath, provider string
	var debug bool

	rootCmd := &cobra.Command{
		Use:   "ai-mr-comment",
		Short: "Generate MR/PR comments using AI",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := loadConfig()
			if cmd.Flags().Changed("provider") {
				cfg.Provider = ApiProvider(provider)
			} else {
				cfg.Provider = ApiProvider(cfg.Provider)
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

			var diffContent string
			var err error
			if diffFilePath != "" {
				diffContent, err = readDiffFromFile(diffFilePath)
			} else {
				diffContent, err = getGitDiff(commit)
			}
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			diffContent = processDiff(diffContent, 4000)
			systemPrompt := NewPromptTemplate().SystemMessage()

			if debug {
				systemTokens := estimateTokens(systemPrompt)
				diffTokens := estimateTokens(diffContent)
				originalLen := len(strings.Split(diffContent, "\n"))
				totalTokens := systemTokens + diffTokens

				fmt.Fprintln(out, "Token estimation:")
				fmt.Fprintf(out, "- System prompt: %d tokens\n", systemTokens)
				fmt.Fprintf(out, "- Diff content: %d tokens (%d lines)\n", diffTokens, originalLen)
				fmt.Fprintf(out, "- Total estimate: %d tokens\n", totalTokens)
				fmt.Fprintln(out, "OpenApi limit: 200,000 tokens")
				fmt.Fprintln(out, "Anthropic's limit: 200,000 tokens")
				return nil
			}

			comment, err := chatFn(cmd.Context(), cfg, cfg.Provider, systemPrompt, diffContent)
			if err != nil {
				if cfg.Provider == Ollama && strings.Contains(err.Error(), "connection refused") {
					return fmt.Errorf("failed to connect to Ollama at %s.\nMake sure Ollama is running (try 'ollama serve') or check your configuration", cfg.OllamaEndpoint)
				}
				return err
			}

			fmt.Fprintln(out)
			fmt.Fprintln(out, "----------------------------------------")
			fmt.Fprintln(out)
			fmt.Fprintln(out, comment)

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
	rootCmd.Flags().BoolVar(&debug, "debug", false, "Estimate token usage")

	return rootCmd
}
