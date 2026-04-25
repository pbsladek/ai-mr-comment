package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// changelogArgs holds the parsed flag values for the changelog subcommand.
type changelogArgs struct {
	commit           string
	diffFilePath     string
	outputPath       string
	provider         string
	modelOverride    string
	format           string
	systemPromptFlag string
	profile          string
	estimate         bool
	autoYes          bool
}

// runChangelog executes the changelog generation logic.
func runChangelog(cmd *cobra.Command, a changelogArgs, chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error)) error {
	cfg, err := loadConfigForProfile(a.profile)
	if err != nil {
		return err
	}
	if cmd.Flags().Changed("provider") {
		cfg.Provider = ApiProvider(a.provider)
	}
	if cmd.Flags().Changed("model") {
		setModelOverride(cfg, a.modelOverride)
	}

	if cfgErr := validateProviderConfig(cfg); cfgErr != nil {
		return cfgErr
	}
	if cancel := applyRequestTimeout(cmd, cfg); cancel != nil {
		defer cancel()
	}

	if a.format != "text" && a.format != "json" {
		return fmt.Errorf("unsupported format %q: must be text or json", a.format)
	}

	diffContent, err := resolveDiff(cmd, a.commit, a.diffFilePath)
	if err != nil {
		return err
	}
	diffContent = processDiff(diffContent, 4000)

	prompt, err := resolveChangelogPrompt(a.systemPromptFlag)
	if err != nil {
		return err
	}

	if a.estimate {
		showCostEstimate(cmd.Context(), cfg, prompt, diffContent, cmd.OutOrStdout())
		if !promptConfirm(cmd.ErrOrStderr(), os.Stdin, a.autoYes) {
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

	return writeChangelogOutput(cmd, cfg, a.outputPath, a.format, entry)
}

// resolveDiff obtains diff content from a file path, commit range, or working tree.
func resolveDiff(cmd *cobra.Command, commit, diffFilePath string) (string, error) {
	var diffContent string
	var err error
	if diffFilePath != "" {
		diffContent, err = readCommandInput(cmd, diffFilePath)
	} else if commandStdinIsPiped(cmd) {
		diffContent, err = readCommandInput(cmd, "-")
	} else {
		if !isGitRepo() {
			return "", fmt.Errorf("not a git repository. Run from inside a git repo or use --file to provide a diff")
		}
		diffContent, err = getGitDiff(commit, false, nil)
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(diffContent) == "" {
		if commit != "" {
			return "", fmt.Errorf("no diff found for commit range %q", commit)
		}
		return "", fmt.Errorf("no diff found. Specify a commit range with --commit or a file with --file")
	}
	return diffContent, nil
}

// resolveChangelogPrompt returns the system prompt, optionally overridden by the flag value.
func resolveChangelogPrompt(systemPromptFlag string) (string, error) {
	if systemPromptFlag == "" {
		return changelogPrompt, nil
	}
	return resolveSystemPrompt(systemPromptFlag)
}

// writeChangelogOutput writes the generated entry to a file or stdout.
func writeChangelogOutput(cmd *cobra.Command, cfg *Config, outputPath, format, entry string) error {
	if outputPath != "" {
		return writeChangelogFile(cfg, outputPath, format, entry)
	}
	return writeChangelogStdout(cmd, cfg, format, entry)
}

// writeChangelogFile persists the changelog entry to disk.
func writeChangelogFile(cfg *Config, outputPath, format, entry string) error {
	var fileContent []byte
	if format == "json" {
		var buf strings.Builder
		if err := json.NewEncoder(&buf).Encode(changelogPayload(cfg, entry)); err != nil {
			return err
		}
		fileContent = []byte(buf.String())
	} else {
		fileContent = []byte(entry + "\n")
	}
	return os.WriteFile(outputPath, fileContent, 0600) //nolint:gosec // G306: 0600 is intentional for user-owned output
}

// writeChangelogStdout writes the changelog entry to the command's stdout.
func writeChangelogStdout(cmd *cobra.Command, cfg *Config, format, entry string) error {
	out := cmd.OutOrStdout()
	if format == "json" {
		return json.NewEncoder(out).Encode(changelogPayload(cfg, entry))
	}
	_, _ = fmt.Fprintln(out, entry)
	return nil
}

// changelogPayload builds the JSON payload struct for changelog output.
func changelogPayload(cfg *Config, entry string) any {
	return struct {
		Changelog string `json:"changelog"`
		Provider  string `json:"provider"`
		Model     string `json:"model"`
	}{
		Changelog: entry,
		Provider:  string(cfg.Provider),
		Model:     getModelName(cfg),
	}
}

// newChangelogCmd returns the changelog subcommand, which generates a
// user-facing changelog entry from a commit range using AI.
func newChangelogCmd(chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error)) *cobra.Command {
	var a changelogArgs

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
			return runChangelog(cmd, a, chatFn)
		},
	}

	cmd.Flags().StringVar(&a.commit, "commit", "", "Commit or commit range to diff (e.g. v1.2.0..HEAD)")
	cmd.Flags().StringVar(&a.diffFilePath, "file", "", "Path to diff file instead of running git diff")
	cmd.Flags().StringVar(&a.outputPath, "output", "", "Write changelog to this file instead of stdout")
	cmd.Flags().StringVar(&a.provider, "provider", "openai", "AI provider (openai, anthropic, gemini, ollama)")
	cmd.Flags().StringVar(&a.modelOverride, "model", "", "Override the model for this run")
	cmd.Flags().StringVar(&a.format, "format", "text", "Output format: text or json")
	cmd.Flags().StringVar(&a.systemPromptFlag, "system-prompt", "", `Override the system prompt for this run. Use @path to read from a file (e.g. --system-prompt=@notes.txt).`)
	cmd.Flags().BoolVar(&a.estimate, "estimate", false, "Show token/cost estimate and prompt for confirmation before calling the API")
	cmd.Flags().BoolVarP(&a.autoYes, "yes", "y", false, "Auto-confirm the cost estimate prompt (use with --estimate)")
	cmd.Flags().StringVar(&a.profile, "profile", "", "Named config profile to activate (defined in ~/.ai-mr-comment.toml under [profile.<name>])")
	return cmd
}
