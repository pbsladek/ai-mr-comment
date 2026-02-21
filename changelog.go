package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// newChangelogCmd returns the changelog subcommand, which generates a
// user-facing changelog entry from a commit range using AI.
func newChangelogCmd(chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error)) *cobra.Command {
	var commit, diffFilePath, outputPath, provider, modelOverride, format, systemPromptFlag string
	var estimate, autoYes bool

	cmd := &cobra.Command{
		Use:   "changelog",
		Short: "Generate a user-facing changelog entry from a commit range",
		Long: `Analyses a git diff (commit range, staged changes, or diff file) and
produces a user-facing changelog entry in Keep a Changelog markdown
format, grouped by Added / Changed / Fixed / Breaking Changes etc.

Examples:
  ai-mr-comment changelog --commit="v1.2.0..HEAD"
  ai-mr-comment changelog --commit="v1.2.0..HEAD" --format=json
  ai-mr-comment changelog --file=my.diff --provider=anthropic`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("provider") {
				cfg.Provider = ApiProvider(provider)
			}
			if cmd.Flags().Changed("model") {
				setModelOverride(cfg, modelOverride)
			}

			if cfgErr := validateProviderConfig(cfg); cfgErr != nil {
				return cfgErr
			}

			if format != "text" && format != "json" {
				return fmt.Errorf("unsupported format %q: must be text or json", format)
			}

			// Obtain diff content from file, commit range, or working tree.
			var diffContent string
			err = nil
			if diffFilePath != "" {
				diffContent, err = readDiffFromFile(diffFilePath)
			} else {
				if !isGitRepo() {
					return fmt.Errorf("not a git repository. Run from inside a git repo or use --file to provide a diff")
				}
				diffContent, err = getGitDiff(commit, false, nil)
			}
			if err != nil {
				return err
			}
			if strings.TrimSpace(diffContent) == "" {
				if commit != "" {
					return fmt.Errorf("no diff found for commit range %q", commit)
				}
				return fmt.Errorf("no diff found. Specify a commit range with --commit or a file with --file")
			}

			diffContent = processDiff(diffContent, 4000)

			prompt := changelogPrompt
			if systemPromptFlag != "" {
				override, spErr := resolveSystemPrompt(systemPromptFlag)
				if spErr != nil {
					return spErr
				}
				prompt = override
			}

			if estimate {
				showCostEstimate(cmd.Context(), cfg, prompt, diffContent, cmd.OutOrStdout())
				if !promptConfirm(cmd.ErrOrStderr(), os.Stdin, autoYes) {
					return nil
				}
			}

			entry, err := timedCall(cfg, "changelog", func() (string, error) {
				return chatFn(cmd.Context(), cfg, cfg.Provider, prompt, diffContent)
			})
			if err != nil {
				if cfg.Provider == Ollama && strings.Contains(err.Error(), "connection refused") {
					return fmt.Errorf("failed to connect to Ollama at %s.\nMake sure Ollama is running (try 'ollama serve') or check your configuration", cfg.OllamaEndpoint)
				}
				return err
			}
			entry = strings.TrimSpace(entry)

			if outputPath != "" {
				var fileContent []byte
				if format == "json" {
					payload := struct {
						Changelog string `json:"changelog"`
						Provider  string `json:"provider"`
						Model     string `json:"model"`
					}{
						Changelog: entry,
						Provider:  string(cfg.Provider),
						Model:     getModelName(cfg),
					}
					var buf strings.Builder
					if encErr := json.NewEncoder(&buf).Encode(payload); encErr != nil {
						return encErr
					}
					fileContent = []byte(buf.String())
				} else {
					fileContent = []byte(entry + "\n")
				}
				return os.WriteFile(outputPath, fileContent, 0600) //nolint:gosec // G306: 0600 is intentional for user-owned output
			}

			out := cmd.OutOrStdout()
			if format == "json" {
				payload := struct {
					Changelog string `json:"changelog"`
					Provider  string `json:"provider"`
					Model     string `json:"model"`
				}{
					Changelog: entry,
					Provider:  string(cfg.Provider),
					Model:     getModelName(cfg),
				}
				if encErr := json.NewEncoder(out).Encode(payload); encErr != nil {
					return encErr
				}
			} else {
				_, _ = fmt.Fprintln(out, entry)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&commit, "commit", "", "Commit or commit range to diff (e.g. v1.2.0..HEAD)")
	cmd.Flags().StringVar(&diffFilePath, "file", "", "Path to diff file instead of running git diff")
	cmd.Flags().StringVar(&outputPath, "output", "", "Write changelog to this file instead of stdout")
	cmd.Flags().StringVar(&provider, "provider", "openai", "AI provider (openai, anthropic, gemini, ollama)")
	cmd.Flags().StringVar(&modelOverride, "model", "", "Override the model for this run")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text or json")
	cmd.Flags().StringVar(&systemPromptFlag, "system-prompt", "", `Override the system prompt for this run. Use @path to read from a file (e.g. --system-prompt=@notes.txt).`)
	cmd.Flags().BoolVar(&estimate, "estimate", false, "Show token/cost estimate and prompt for confirmation before calling the API")
	cmd.Flags().BoolVarP(&autoYes, "yes", "y", false, "Auto-confirm the cost estimate prompt (use with --estimate)")
	return cmd
}
