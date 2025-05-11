package main

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func main() {
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
			diff = processDiff(diff, 4000)
			host := detectGitHost()
			prompt := NewPromptTemplate(host).SystemMessage()

			if debug {
				systemTokens := estimateTokens(prompt)
				diffTokens := estimateTokens(diff)
				originalLen := len(strings.Split(diff, "\n"))
				totalTokens := systemTokens + diffTokens
				fmt.Println("Token estimation:")
				fmt.Printf("- System prompt: %d tokens\n", systemTokens)
				fmt.Printf("- Diff content: %d tokens (%d lines)\n", diffTokens, originalLen)
				fmt.Printf("- Total estimate: %d tokens\n", totalTokens)
				fmt.Println("OpenApi limit: 200,000 tokens")
				fmt.Println("Claude's limit: 200,000 tokens")
				return nil
			}

			comment, err := chatCompletions(cfg, cfg.Provider, prompt, diff)
			if err != nil {
				return err
			}
			if outputPath != "" {
				return os.WriteFile(outputPath, []byte(comment), 0644)
			}
			fmt.Println(comment)
			return nil
		},
	}

	rootCmd.Flags().StringVar(&commit, "commit", "", "Commit or commit range")
	rootCmd.Flags().StringVar(&filePath, "file", "", "Path to diff file")
	rootCmd.Flags().StringVar(&outputPath, "output", "", "Output file path")
	rootCmd.Flags().StringVar(&provider, "provider", "openai", "API provider (openai or claude)")
	rootCmd.Flags().BoolVar(&debug, "debug", false, "Estimate token usage")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
