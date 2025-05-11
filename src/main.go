package main

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCmd(chatCompletions).Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func newRootCmd(chatFn func(*Config, ApiProvider, string, string) (string, error)) *cobra.Command {
	var commit, filePath, outputPath, provider string
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

			if cfg.Provider != OpenAI && cfg.Provider != Anthropic && cfg.Provider != Ollama {
				return errors.New("unsupported provider: " + string(cfg.Provider))
			}

			var diff string
			var err error
			if filePath != "" {
				diff, err = readDiffFromFile(filePath)
			} else {
				diff, err = getGitDiff(commit)
			}
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			diff = processDiff(diff, 4000)
			host := detectGitHost()
			prompt := NewPromptTemplate(host).SystemMessage()

			if debug {
				systemTokens := estimateTokens(prompt)
				diffTokens := estimateTokens(diff)
				originalLen := len(strings.Split(diff, "\n"))
				totalTokens := systemTokens + diffTokens

				fmt.Fprintln(out, "Token estimation:")
				fmt.Fprintf(out, "- System prompt: %d tokens\n", systemTokens)
				fmt.Fprintf(out, "- Diff content: %d tokens (%d lines)\n", diffTokens, originalLen)
				fmt.Fprintf(out, "- Total estimate: %d tokens\n", totalTokens)
				fmt.Fprintln(out, "OpenApi limit: 200,000 tokens")
				fmt.Fprintln(out, "Anthropic's limit: 200,000 tokens")
				return nil
			}

			spinnerMsg := fmt.Sprintf("calling %s api", cfg.Provider)
			err = withSpinner(spinnerMsg, func() error {
				comment, err := chatFn(cfg, cfg.Provider, prompt, diff)
				if err == nil {
					fmt.Fprintln(out)
					fmt.Fprintln(out, "----------------------------------------")
					fmt.Fprintln(out)
					fmt.Fprintln(out, comment)
				}

				if outputPath != "" {
					return os.WriteFile(outputPath, []byte(comment), 0644)
				}

				return err
			})
			if err != nil {
				return err
			}
			return nil
		},
	}

	rootCmd.Flags().StringVar(&commit, "commit", "", "Commit or commit range")
	rootCmd.Flags().StringVar(&filePath, "file", "", "Path to diff file")
	rootCmd.Flags().StringVar(&outputPath, "output", "", "Output file path")
	rootCmd.Flags().StringVar(&provider, "provider", "openai", "API provider (openai or claude)")
	rootCmd.Flags().BoolVar(&debug, "debug", false, "Estimate token usage")

	return rootCmd
}

func withSpinner(label string, f func() error) error {
	done := make(chan struct{})
	go func() {
		spin := []rune(`|/-\`)
		i := 0
		for {
			select {
			case <-done:
				return
			default:
				fmt.Printf("\r%s %c", label, spin[i%len(spin)])
				i++
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()

	// start := time.Now()
	err := f()
	close(done)
	fmt.Println()
	// fmt.Printf("\r%s done in %s\n", label, time.Since(start).Truncate(time.Millisecond))
	return err
}
